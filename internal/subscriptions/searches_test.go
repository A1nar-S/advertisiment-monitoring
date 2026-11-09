package subscriptions

import (
	"context"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
)

// Needs a real Postgres: TEST_DATABASE_URL=postgres://... go test ./internal/subscriptions
func newTestService(t *testing.T, src sources.Source) (*Service, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS settings, sessions, login_tokens, matches, searches, channels, users, subscriptions_migrations`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(pool, sources.NewRegistry(src), 3).WithLimits(3, []string{"discord:admin-1"})
	for range 2 { // migrations are idempotent
		if err := svc.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return svc, pool
}

// user makes a Discord user with a DM channel.
func user(t *testing.T, svc *Service, discordID string) (Owner, ChannelView) {
	t.Helper()
	ctx := context.Background()
	o, err := svc.PlatformOwner(ctx, "discord", discordID, "name-"+discordID)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := svc.EnsureChannel(ctx, o, "discord", "dm-"+discordID, "Private messages")
	if err != nil {
		t.Fatal(err)
	}
	return o, ch
}

func TestSearchLifecycle(t *testing.T) {
	svc, _ := newTestService(t, fakeSource{})
	ctx := context.Background()
	alice, aliceDM := user(t, svc, "111")
	bob, bobDM := user(t, svc, "222")

	// The same chat account is the same user; the same ID elsewhere is not.
	if again, _ := svc.PlatformOwner(ctx, "discord", "111", "Alice renamed"); again.UserID != alice.UserID {
		t.Fatalf("second contact made a new user: %d vs %d", again.UserID, alice.UserID)
	}
	if tg, _ := svc.PlatformOwner(ctx, "telegram", "111", "Someone"); tg.UserID == alice.UserID {
		t.Fatal("telegram:111 got discord:111's account")
	}

	in := SearchInput{Name: "cheap centre", Category: "flats/centre", ChannelID: aliceDM.ID, Origin: "bot",
		Filters: map[string]any{"price": map[string]any{"max": 450.0}}}
	v, err := svc.CreateSearch(ctx, alice, in)
	if err != nil {
		t.Fatal(err)
	}
	if v.Category != "flats/centre" || !reflect.DeepEqual(v.Filters, in.Filters) || !v.Enabled || v.Channel.Target != "dm-111" {
		t.Errorf("created %+v", v)
	}

	// Scraper and matcher see it at once.
	feeds, err := svc.ActiveFeeds(ctx, "ss.com")
	if err != nil || !reflect.DeepEqual(feeds, []string{"flats/centre"}) {
		t.Fatalf("feeds = %v, err = %v", feeds, err)
	}
	l := events.ListingDiscovered{Source: "ss.com", ExternalID: "1", Location: "flats/centre", PriceValue: ptr(400.0)}
	ns, err := svc.Notifications(ctx, l)
	if err != nil || len(ns) != 1 || ns[0].Channel.Target != "dm-111" || ns[0].SearchName != "cheap centre" {
		t.Fatalf("notifications = %+v, err = %v", ns, err)
	}
	if err := svc.Record(ctx, ns); err != nil {
		t.Fatal(err)
	}
	if again, _ := svc.Notifications(ctx, l); len(again) != 0 {
		t.Errorf("after Record, redelivery produced %d notifications, want 0", len(again))
	}
	if ms, err := svc.RecentMatches(ctx, alice, 0, 10); err != nil || len(ms) != 1 || ms[0].ExternalID != "1" {
		t.Errorf("matches = %+v, %v", ms, err)
	}
	if ms, err := svc.RecentMatches(ctx, alice, v.ID, 10); err != nil || len(ms) != 1 {
		t.Errorf("matches of the search = %+v, %v", ms, err)
	}
	if ms, err := svc.RecentMatches(ctx, alice, v.ID+1000, 10); err != nil || len(ms) != 0 {
		t.Errorf("matches of another search = %+v, %v", ms, err)
	}

	// Invalid input is an InvalidSearch with a message for the user.
	var inv *sources.InvalidSearch
	for _, bad := range []SearchInput{
		{Name: "", Category: "x", ChannelID: aliceDM.ID},
		{Name: "typo", Category: "x", ChannelID: aliceDM.ID, Filters: map[string]any{"nope": 1}},
		{Name: "not my channel", Category: "x", ChannelID: bobDM.ID},
		{Name: "cheap centre", Category: "x", ChannelID: aliceDM.ID}, // name taken
	} {
		if _, err := svc.CreateSearch(ctx, alice, bad); !errors.As(err, &inv) {
			t.Errorf("%+v: err = %v, want InvalidSearch", bad, err)
		}
	}

	// Bob cannot see or touch Alice's search; it looks like it does not exist.
	if _, err := svc.GetSearch(ctx, bob, v.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob get: %v", err)
	}
	if err := svc.DeleteSearch(ctx, bob, v.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob delete: %v", err)
	}
	if _, err := svc.UpdateSearch(ctx, bob, v.ID, in); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob update: %v", err)
	}

	// Saving by name edits (the bot's "/watch" again).
	in.Filters = map[string]any{"price": map[string]any{"max": 500.0}}
	in.ChannelID = 0 // keep the channel
	edited, created, err := svc.SaveSearchByName(ctx, alice, in)
	if err != nil || created || edited.ID != v.ID || edited.Channel.ID != aliceDM.ID {
		t.Fatalf("edit by name: %+v created=%v err=%v", edited, created, err)
	}

	// Paused searches neither scrape nor match.
	if err := svc.SetEnabled(ctx, alice, v.ID, false); err != nil {
		t.Fatal(err)
	}
	if feeds, _ := svc.ActiveFeeds(ctx, "ss.com"); len(feeds) != 0 {
		t.Errorf("paused search still scraped: %v", feeds)
	}
	l.ExternalID = "2"
	if ns, _ := svc.Notifications(ctx, l); len(ns) != 0 {
		t.Errorf("paused search notified: %+v", ns)
	}

	// The per-user limit (3 here); admins are exempt and see everyone's.
	for _, name := range []string{"b", "c"} {
		if _, err := svc.CreateSearch(ctx, alice, SearchInput{Name: name, Category: "x", ChannelID: aliceDM.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.CreateSearch(ctx, alice, SearchInput{Name: "d", Category: "x", ChannelID: aliceDM.ID}); !errors.As(err, &inv) || !strings.Contains(err.Error(), "limit") {
		t.Errorf("4th search: err = %v, want the limit", err)
	}
	admin, _ := user(t, svc, "admin-1")
	if !admin.Admin {
		t.Fatal("admin-1 is not an admin")
	}
	if all, _ := svc.ListSearches(ctx, admin, true); len(all) != 3 {
		t.Errorf("admin sees %d searches, want 3", len(all))
	}
	if mine, _ := svc.ListSearches(ctx, alice, true); len(mine) != 3 {
		t.Errorf("non-admin asking for all sees %d, want only own 3", len(mine))
	}
	if own, _ := svc.ListSearches(ctx, bob, true); len(own) != 0 {
		t.Errorf("bob sees %d searches, want 0", len(own))
	}

	if err := svc.DeleteSearch(ctx, admin, v.ID); err != nil {
		t.Errorf("admin delete: %v", err)
	}
	if _, err := svc.GetSearch(ctx, alice, v.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted search still there: %v", err)
	}
}

// enrichingSource reads "ad pages" by counting calls.
type enrichingSource struct {
	fakeSource
	calls int
	err   error
}

func (*enrichingSource) EnrichableKeys() []string { return []string{"gearbox"} }
func (e *enrichingSource) Enrich(_ context.Context, l events.ListingDiscovered) (events.ListingDiscovered, error) {
	e.calls++
	if e.err != nil {
		return l, e.err
	}
	out := l
	out.Attrs = maps(l.Attrs)
	out.Attrs["gearbox"] = "Automāts"
	return out, nil
}

func TestNotificationsEnrichOnce(t *testing.T) {
	src := &enrichingSource{}
	svc, _ := newTestService(t, src)
	ctx := context.Background()
	alice, aliceDM := user(t, svc, "111")
	bob, bobDM := user(t, svc, "222")
	add := func(o Owner, ch ChannelView, name string, filters map[string]any) {
		t.Helper()
		if _, err := svc.CreateSearch(ctx, o, SearchInput{Name: name, Category: "cars", ChannelID: ch.ID, Filters: filters}); err != nil {
			t.Fatal(err)
		}
	}
	// Two users, four searches; three need the gearbox, one wants manual.
	add(alice, aliceDM, "auto", map[string]any{"gearbox": []any{"Automāts"}})
	add(alice, aliceDM, "cheap", map[string]any{"price": map[string]any{"max": 6000.0}})
	add(bob, bobDM, "auto-or-manual", map[string]any{"gearbox": []any{"Automāts", "Manuāla"}, "year": map[string]any{"min": 2010.0}})
	add(bob, bobDM, "manual", map[string]any{"gearbox": []any{"Manuāla"}})

	l := events.ListingDiscovered{Source: "ss.com", ExternalID: "7", Location: "cars", PriceValue: ptr(5000.0), Attrs: map[string]any{"year": 2015.0}}
	got, err := svc.Notifications(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, n := range got {
		names = append(names, n.SearchName)
		if v, _ := n.Listing.Str("gearbox"); v != "Automāts" {
			t.Errorf("%s: notification lacks the enriched gearbox", n.SearchName)
		}
	}
	slices.Sort(names)
	if !reflect.DeepEqual(names, []string{"auto", "auto-or-manual", "cheap"}) || src.calls != 1 {
		t.Errorf("matched %v with %d ad-page reads, want 3 searches and 1 read", names, src.calls)
	}

	// The ad page failing: candidates still hear about it, flagged.
	src.err = errors.New("403")
	l.ExternalID = "8"
	got, err = svc.Notifications(ctx, l)
	if err != nil || len(got) != 4 {
		t.Fatalf("unconfirmed: %d notifications, err %v; want all 4", len(got), err)
	}
	if f := got[len(got)-1].Listing.Fields; len(f) == 0 || !strings.Contains(f[len(f)-1].Value, "gearbox") {
		t.Errorf("unconfirmed listing not flagged: %+v", f)
	}

	// A removed ad matches nothing that needed its page.
	src.err = sources.ErrGone
	l.ExternalID = "9"
	got, _ = svc.Notifications(ctx, l)
	if len(got) != 1 || got[0].SearchName != "cheap" {
		t.Errorf("gone: %+v, want only the cheap search", got)
	}
}

func TestLogin(t *testing.T) {
	svc, pool := newTestService(t, fakeSource{})
	ctx := context.Background()
	alice, _ := user(t, svc, "111")

	token, err := svc.NewLoginToken(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	session, o, err := svc.Login(ctx, token)
	if err != nil || o.UserID != alice.UserID || o.Platform != "discord" || o.ExternalID != "111" {
		t.Fatalf("login: %+v, %v", o, err)
	}
	if _, _, err := svc.Login(ctx, token); !errors.Is(err, ErrBadToken) {
		t.Errorf("second use of a login link: %v, want ErrBadToken", err)
	}
	if o, err := svc.SessionOwner(ctx, session); err != nil || o.UserID != alice.UserID {
		t.Errorf("session: %+v, %v", o, err)
	}

	// Expired links do not work; only hashes are stored.
	late, _ := svc.NewLoginToken(ctx, alice)
	if _, err := pool.Exec(ctx, `UPDATE login_tokens SET expires_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Login(ctx, late); !errors.Is(err, ErrBadToken) {
		t.Errorf("expired link: %v", err)
	}
	var plain int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE token_hash = $1`, session).Scan(&plain)
	if plain != 0 {
		t.Error("a session token is stored in plain text")
	}

	if err := svc.Logout(ctx, session); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SessionOwner(ctx, session); !errors.Is(err, ErrBadToken) {
		t.Errorf("after logout: %v", err)
	}
}

func TestStatusChannel(t *testing.T) {
	svc, _ := newTestService(t, fakeSource{})
	ctx := context.Background()
	alice, _ := user(t, svc, "111")
	admin, _ := user(t, svc, "admin-1")

	if _, ok, err := svc.StatusChannel(ctx); ok || err != nil {
		t.Fatalf("unset: ok=%v err=%v", ok, err)
	}
	if err := svc.SetStatusChannel(ctx, alice, events.Channel{Kind: "discord", Target: "42"}); !errors.Is(err, ErrNotAdmin) {
		t.Errorf("non-admin: %v, want ErrNotAdmin", err)
	}
	for _, want := range []events.Channel{{Kind: "discord", Target: "42"}, {Kind: "discord"}} {
		if err := svc.SetStatusChannel(ctx, admin, want); err != nil {
			t.Fatal(err)
		}
		if got, ok, err := svc.StatusChannel(ctx); !ok || err != nil || got != want {
			t.Errorf("got %+v ok=%v err=%v, want %+v", got, ok, err, want)
		}
	}
}
