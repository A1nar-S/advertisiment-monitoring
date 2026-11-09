package bot

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/scraping/ss"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions"
)

type fakeMessenger struct {
	sent     []string
	failSend bool
}

func (*fakeMessenger) Platform() string { return "chat" }
func (*fakeMessenger) DirectChat(_ context.Context, userID string) (string, error) {
	return "direct-" + userID, nil
}
func (m *fakeMessenger) Send(_ context.Context, target, text string) error {
	if m.failSend {
		return errors.New("closed")
	}
	m.sent = append(m.sent, target+": "+text)
	return nil
}
func (*fakeMessenger) Mention(target string) string { return "#" + target }
func (*fakeMessenger) ChannelName(_ context.Context, target string) string {
	return "#" + target
}

// fakeSearches keeps searches in memory; a filter "make: Skodaa" is invalid.
type fakeSearches struct {
	searches map[int64]*subscriptions.SearchView
	channels map[string]int64
	labels   []string
	nextID   int64
	saved    []subscriptions.SearchInput
	matches  []subscriptions.MatchRef
	status   *events.Channel
	admin    bool
}

func newFakeSearches() *fakeSearches {
	return &fakeSearches{searches: map[int64]*subscriptions.SearchView{}, channels: map[string]int64{}}
}

func (f *fakeSearches) PlatformOwner(_ context.Context, platform, id, name string) (subscriptions.Owner, error) {
	return subscriptions.Owner{UserID: 1, Platform: platform, ExternalID: id, Name: name, Admin: f.admin}, nil
}
func (f *fakeSearches) StatusChannel(context.Context) (events.Channel, bool, error) {
	if f.status == nil {
		return events.Channel{}, false, nil
	}
	return *f.status, true, nil
}
func (f *fakeSearches) SetStatusChannel(_ context.Context, o subscriptions.Owner, ch events.Channel) error {
	if !o.Admin {
		return subscriptions.ErrNotAdmin
	}
	f.status = &ch
	return nil
}
func (f *fakeSearches) EnsureChannel(_ context.Context, _ subscriptions.Owner, kind, target, label string) (subscriptions.ChannelView, error) {
	key := kind + "/" + target
	if f.channels[key] == 0 {
		f.channels[key] = int64(len(f.channels) + 1)
		f.labels = append(f.labels, label)
	}
	return subscriptions.ChannelView{ID: f.channels[key], Kind: kind, Target: target, Label: label}, nil
}
func (f *fakeSearches) SaveSearchByName(_ context.Context, _ subscriptions.Owner, in subscriptions.SearchInput) (subscriptions.SearchView, bool, error) {
	if ms, _ := in.Filters["make"].([]any); slices.Contains(ms, any("Skodaa")) {
		return subscriptions.SearchView{}, false, &sources.InvalidSearch{Msg: `unknown make "Skodaa" (did you mean "Skoda"?)`}
	}
	f.saved = append(f.saved, in)
	target := ""
	for k, id := range f.channels {
		if id == in.ChannelID {
			target = k[strings.IndexByte(k, '/')+1:]
		}
	}
	for _, v := range f.searches {
		if v.Name == in.Name {
			v.Filters = in.Filters
			return *v, false, nil
		}
	}
	f.nextID++
	v := &subscriptions.SearchView{ID: f.nextID, Name: in.Name, Category: in.Category, Filters: in.Filters, Enabled: true,
		Channel: subscriptions.ChannelView{ID: in.ChannelID, Target: target, Label: f.labels[in.ChannelID-1]}}
	f.searches[v.ID] = v
	return *v, true, nil
}
func (f *fakeSearches) UpdateSearch(_ context.Context, _ subscriptions.Owner, id int64, in subscriptions.SearchInput) (subscriptions.SearchView, error) {
	if ms, _ := in.Filters["make"].([]any); slices.Contains(ms, any("Skodaa")) {
		return subscriptions.SearchView{}, &sources.InvalidSearch{Msg: `unknown make "Skodaa" (did you mean "Skoda"?)`}
	}
	v, ok := f.searches[id]
	if !ok {
		return subscriptions.SearchView{}, subscriptions.ErrNotFound
	}
	f.saved = append(f.saved, in)
	v.Filters = in.Filters
	if in.ChannelID != 0 {
		for k, cid := range f.channels {
			if cid == in.ChannelID {
				v.Channel = subscriptions.ChannelView{ID: cid, Target: k[strings.IndexByte(k, '/')+1:], Label: f.labels[cid-1]}
			}
		}
	}
	return *v, nil
}
func (f *fakeSearches) RecentMatches(_ context.Context, _ subscriptions.Owner, searchID int64, limit int) ([]subscriptions.MatchRef, error) {
	var out []subscriptions.MatchRef
	for _, m := range f.matches {
		if (searchID == 0 || m.SearchID == searchID) && len(out) < limit {
			out = append(out, m)
		}
	}
	return out, nil
}
func (f *fakeSearches) FindSearch(_ context.Context, _ subscriptions.Owner, name string) (subscriptions.SearchView, error) {
	for _, v := range f.searches {
		if v.Name == name {
			return *v, nil
		}
	}
	return subscriptions.SearchView{}, subscriptions.ErrNotFound
}
func (f *fakeSearches) GetSearch(_ context.Context, _ subscriptions.Owner, id int64) (subscriptions.SearchView, error) {
	if v, ok := f.searches[id]; ok {
		return *v, nil
	}
	return subscriptions.SearchView{}, subscriptions.ErrNotFound
}
func (f *fakeSearches) ListSearches(context.Context, subscriptions.Owner, bool) ([]subscriptions.SearchView, error) {
	var out []subscriptions.SearchView
	for id := int64(1); id <= f.nextID; id++ {
		if v, ok := f.searches[id]; ok {
			out = append(out, *v)
		}
	}
	return out, nil
}
func (f *fakeSearches) SetEnabled(_ context.Context, _ subscriptions.Owner, id int64, enabled bool) error {
	f.searches[id].Enabled = enabled
	return nil
}
func (f *fakeSearches) DeleteSearch(_ context.Context, _ subscriptions.Owner, id int64) error {
	delete(f.searches, id)
	return nil
}
func (*fakeSearches) NewLoginToken(context.Context, subscriptions.Owner) (string, error) {
	return "tok", nil
}
func (*fakeSearches) Categories() map[string][]sources.Category {
	return map[string][]sources.Category{subscriptions.DefaultSource: ss.NewClient(time.Second).Categories()}
}

// fakeAds stores ad "1" only.
type fakeAds struct{}

func (fakeAds) Ads(_ context.Context, _ string, ids []string) (map[string]Ad, error) {
	out := map[string]Ad{}
	if slices.Contains(ids, "1") {
		out["1"] = Ad{Title: "Skoda Octavia [RS]", URL: "https://ss.com/1.html", Price: "6 500 €", Location: "Rīga"}
	}
	return out, nil
}

var now = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func newBot() (*Bot, *fakeMessenger, *fakeSearches) {
	m, s := &fakeMessenger{}, newFakeSearches()
	b := New(m, s, fakeAds{}, "https://ads.example/", 10)
	b.now = func() time.Time { return now }
	return b, m, s
}

var alice = User{ID: "u1", Name: "Alice"}

func TestCommandsUseTheCatalog(t *testing.T) {
	b, _, _ := newBot()
	params := map[string]Param{}
	for _, c := range b.Commands() {
		if c.Name != CmdWatch {
			continue
		}
		for _, sub := range c.Subcommands {
			for _, p := range sub.Params {
				params[sub.Name+"."+p.Name] = p
			}
		}
	}
	if g := params["cars.gearbox"]; !reflect.DeepEqual(g.Choices, []string{"Automāts", "Manuāla"}) {
		t.Errorf("gearbox choices = %v", g.Choices)
	}
	if d := params["flats.deal"]; !d.Required || len(d.Choices) != 5 {
		t.Errorf("deal = %+v", d)
	}
	if m := params["cars.make"]; !m.Complete {
		t.Error("make should autocomplete")
	}
	if k, ok := params["cars.keywords"]; !ok || k.Kind != Text || k.Complete {
		t.Errorf("keywords = %+v, %v", k, ok)
	}

	// edit has the same filters, all optional, plus clear.
	var edit Command
	for _, c := range b.Commands() {
		if c.Name == CmdEdit {
			edit = c
		}
	}
	if len(edit.Subcommands) != 2 {
		t.Fatalf("edit = %+v", edit)
	}
	for _, sub := range edit.Subcommands {
		var names []string
		for _, p := range sub.Params {
			names = append(names, p.Name)
			if p.Required != (p.Name == "name") {
				t.Errorf("edit %s: %s required = %v", sub.Name, p.Name, p.Required)
			}
		}
		if !slices.Contains(names, "clear") || len(sub.Params) > 25 {
			t.Errorf("edit %s params = %v", sub.Name, names)
		}
	}
}

func TestFiltersFromArgs(t *testing.T) {
	got := FiltersFromArgs(map[string]any{
		"name": "x", "channel": "c", "make": "Skoda, Toyota,", "price_max": 7000.0,
		"year_min": "2012", "mileage_max": 200000.0, "body": " ",
	})
	want := map[string]any{
		"make":       []any{"Skoda", "Toyota"},
		"price":      map[string]any{"max": 7000.0},
		"year":       map[string]any{"min": 2012.0},
		"mileage_km": map[string]any{"max": 200000.0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestWatch(t *testing.T) {
	b, m, s := newBot()
	ctx := context.Background()
	call := Call{Command: CmdWatch, Sub: "cars", Args: map[string]any{"name": "Parents", "make": "Skoda", "price_max": 7000.0}}

	r := b.Run(ctx, alice, call)
	if !strings.Contains(r.Text, "Saved **Parents**") || !strings.Contains(r.Text, "private messages") {
		t.Errorf("reply = %q", r.Text)
	}
	in := s.saved[0]
	if in.Category != "cars" || in.Origin != "chat" || !reflect.DeepEqual(in.Filters["make"], []any{"Skoda"}) {
		t.Errorf("saved %+v", in)
	}
	if len(m.sent) != 1 || !strings.HasPrefix(m.sent[0], "direct-u1: 🔔") {
		t.Errorf("confirmation = %v", m.sent)
	}

	// A channel the user can't post in is refused.
	call.Args["channel"] = "cars-chan"
	if r := b.Run(ctx, alice, call); !strings.Contains(r.Text, "you can't post in #cars-chan") || s.saved[len(s.saved)-1].ChannelID != in.ChannelID {
		t.Errorf("unpostable channel: %q", r.Text)
	}

	// Same name again edits; a group channel is mentioned.
	call.Postable = map[string]bool{"cars-chan": true}
	if r := b.Run(ctx, alice, call); !strings.Contains(r.Text, "Updated") || !strings.Contains(r.Text, "#cars-chan") {
		t.Errorf("edit reply = %q", r.Text)
	}
	call.Postable = nil

	// Mistakes come back as the reply; an unreachable target is a warning.
	bad := Call{Command: CmdWatch, Sub: "cars", Args: map[string]any{"name": "x", "make": "Skodaa"}}
	if r := b.Run(ctx, alice, bad); r.Text != `❌ unknown make "Skodaa" (did you mean "Skoda"?)` {
		t.Errorf("invalid reply = %q", r.Text)
	}
	m.failSend = true
	delete(call.Args, "channel")
	call.Args["name"] = "Second"
	if r := b.Run(ctx, alice, call); !strings.Contains(r.Text, "can't message you privately") {
		t.Errorf("closed DMs reply = %q", r.Text)
	}
}

func TestSearchesMenus(t *testing.T) {
	b, _, s := newBot()
	ctx := context.Background()
	for _, n := range []string{"A", "B"} {
		b.Run(ctx, alice, Call{Command: CmdWatch, Sub: "cars", Args: map[string]any{"name": n}})
	}
	r := b.Run(ctx, alice, Call{Command: CmdSearches})
	if !strings.Contains(r.Text, "(2/10)") || len(r.Menus) != 2 || r.Menus[0].Action != ActionToggle || len(r.Menus[0].Options) != 2 {
		t.Fatalf("listing = %+v", r)
	}
	if !strings.Contains(r.Text, "private") {
		t.Errorf("direct-chat searches should say private: %q", r.Text)
	}

	r = b.Act(ctx, alice, ActionToggle, "1")
	if s.searches[1].Enabled || !strings.Contains(r.Text, "Paused **A**") || !strings.Contains(r.Menus[0].Options[0].Label, "Resume A") {
		t.Errorf("after pause: %+v", r)
	}
	r = b.Act(ctx, alice, ActionDelete, "2")
	if _, ok := s.searches[2]; ok || !strings.Contains(r.Text, "Deleted **B**") {
		t.Errorf("after delete: %+v", r)
	}
	if r := b.Act(ctx, alice, ActionDelete, "2"); !strings.Contains(r.Text, "gone") {
		t.Errorf("deleting twice: %q", r.Text)
	}

	if r := b.Run(ctx, alice, Call{Command: CmdUnwatch, Args: map[string]any{"name": "A"}}); !strings.Contains(r.Text, "Deleted **A**") {
		t.Errorf("unwatch: %q", r.Text)
	}
	if r := b.Run(ctx, alice, Call{Command: CmdUnwatch, Args: map[string]any{"name": "nope"}}); r.Text != "❌ No such search." {
		t.Errorf("unwatch unknown: %q", r.Text)
	}
	if r := b.Run(ctx, alice, Call{Command: CmdDashboard}); !strings.Contains(r.Text, "https://ads.example/api/login?token=tok") {
		t.Errorf("dashboard: %q", r.Text)
	}
}

func TestComplete(t *testing.T) {
	b, _, _ := newBot()
	ctx := context.Background()
	cars := func(args map[string]any) Call { return Call{Command: CmdWatch, Sub: "cars", Args: args} }

	got := b.Complete(ctx, alice, cars(map[string]any{"make": "Skoda, To"}), "make")
	if !slices.Contains(got[:2], "Skoda, Toyota") || slices.ContainsFunc(got, func(s string) bool { return !strings.HasPrefix(s, "Skoda, ") }) {
		t.Errorf("make = %v: want prefix matches first, all keeping \"Skoda, \"", got)
	}
	got = b.Complete(ctx, alice, cars(map[string]any{"make": "Volkswagen", "model": "pass"}), "model")
	if !slices.Contains(got, "Passat (B5)") || slices.Contains(got, "Octavia") {
		t.Errorf("VW models = %v", got)
	}
	got = b.Complete(ctx, alice, cars(map[string]any{"fuel": "dize"}), "fuel")
	if !reflect.DeepEqual(got, []string{"Dīzelis"}) {
		t.Errorf("fuel = %v (diacritics must not matter)", got)
	}
	flats := Call{Command: CmdWatch, Sub: "flats", Args: map[string]any{"region": "Rīga", "district": "Centrs, tei"}}
	if got := b.Complete(ctx, alice, flats, "district"); len(got) == 0 || got[0] != "Centrs, Teika" {
		t.Errorf("districts = %v", got)
	}
	if got := b.Complete(ctx, alice, cars(map[string]any{"make": ""}), "make"); len(got) != MaxSuggestions {
		t.Errorf("empty make: %d suggestions, want %d", len(got), MaxSuggestions)
	}
}

func TestEdit(t *testing.T) {
	b, m, s := newBot()
	ctx := context.Background()
	b.Run(ctx, alice, Call{Command: CmdWatch, Sub: "cars", Args: map[string]any{
		"name": "Skoda", "make": "Skoda", "model": "Octavia", "price_min": 2000.0, "price_max": 7000.0, "color": "Balta",
	}})
	edit := func(args map[string]any) Reply {
		args["name"] = "Skoda"
		return b.Run(ctx, alice, Call{Command: CmdEdit, Sub: "cars", Args: args, Postable: map[string]bool{"cars-chan": true}})
	}
	if r := edit(map[string]any{"channel": "news"}); !strings.Contains(r.Text, "you can't post in #news") {
		t.Errorf("unpostable channel: %q", r.Text)
	}

	// Only what is given changes; one bound of a range keeps the other.
	r := edit(map[string]any{"price_max": 9000.0, "region": "Rīga"})
	want := map[string]any{
		"make": []any{"Skoda"}, "model": []any{"Octavia"}, "color": []any{"Balta"}, "region": []any{"Rīga"},
		"price": map[string]any{"min": 2000.0, "max": 9000.0},
	}
	if got := s.searches[1].Filters; !reflect.DeepEqual(got, want) {
		t.Errorf("filters = %v\nwant      %v", got, want)
	}
	if !strings.Contains(r.Text, "Updated **Skoda**") || strings.Contains(r.Text, "Alerts now go") {
		t.Errorf("reply = %q", r.Text)
	}
	if s.saved[len(s.saved)-1].ChannelID != 0 {
		t.Error("an edit without channel must keep the channel")
	}

	// clear removes filters, bounds or whole ranges; case doesn't matter.
	edit(map[string]any{"clear": "Model, price_min, color"})
	if got := s.searches[1].Filters; got["model"] != nil || got["color"] != nil || !reflect.DeepEqual(got["price"], map[string]any{"max": 9000.0}) {
		t.Errorf("after clear: %v", got)
	}
	edit(map[string]any{"clear": "price"})
	if _, ok := s.searches[1].Filters["price"]; ok {
		t.Errorf("clear price left %v", s.searches[1].Filters)
	}

	// Moving to a channel and back to private messages says hello there.
	m.sent = nil
	if r := edit(map[string]any{"channel": "cars-chan"}); !strings.Contains(r.Text, "Alerts now go to #cars-chan") || s.searches[1].Channel.Target != "cars-chan" {
		t.Errorf("move to channel: %q, %+v", r.Text, s.searches[1].Channel)
	}
	if r := edit(map[string]any{"clear": "channel"}); !strings.Contains(r.Text, "your private messages") || s.searches[1].Channel.Target != "direct-u1" {
		t.Errorf("back to private: %q, %+v", r.Text, s.searches[1].Channel)
	}
	if len(m.sent) != 2 {
		t.Errorf("hellos = %v", m.sent)
	}

	// Mistakes.
	for _, tc := range []struct {
		args map[string]any
		msg  string
	}{
		{map[string]any{}, "nothing to change"},
		{map[string]any{"clear": "colour"}, `can't clear "colour"`},
		{map[string]any{"make": "Skodaa"}, "unknown make"},
	} {
		if r := edit(tc.args); !strings.HasPrefix(r.Text, "❌") || !strings.Contains(r.Text, tc.msg) {
			t.Errorf("edit %v = %q, want %q", tc.args, r.Text, tc.msg)
		}
	}
	if r := b.Run(ctx, alice, Call{Command: CmdEdit, Sub: "flats", Args: map[string]any{"name": "Skoda", "rooms_min": 2.0}}); !strings.Contains(r.Text, "use **edit cars**") {
		t.Errorf("wrong category: %q", r.Text)
	}
	if r := b.Run(ctx, alice, Call{Command: CmdEdit, Sub: "cars", Args: map[string]any{"name": "nope", "price_max": 1.0}}); r.Text != "❌ No such search." {
		t.Errorf("unknown search: %q", r.Text)
	}
}

func TestMatches(t *testing.T) {
	b, _, s := newBot()
	ctx := context.Background()
	matches := func(args map[string]any) string { return b.Run(ctx, alice, Call{Command: CmdMatches, Args: args}).Text }
	for _, n := range []string{"Skoda", "Flat"} {
		b.Run(ctx, alice, Call{Command: CmdWatch, Sub: "cars", Args: map[string]any{"name": n}})
	}
	if r := matches(map[string]any{}); !strings.HasPrefix(r, "No alerts yet") {
		t.Errorf("none: %q", r)
	}

	s.matches = []subscriptions.MatchRef{
		{SearchID: 1, SearchName: "Skoda", Source: "ss.com", ExternalID: "1", At: now.Add(-3 * time.Hour)},
		{SearchID: 2, SearchName: "Flat", Source: "ss.com", ExternalID: "2", At: now.Add(-72 * time.Hour)},
	}
	r := matches(map[string]any{})
	for _, want := range []string{
		"**Your latest alerts:**",
		`• [Skoda Octavia \[RS\]](<https://ss.com/1.html>) · 6 500 € · Rīga · 3h ago — Skoda`,
		"• ad 2 (no longer stored) · 3d ago — Flat",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("all matches: %q\nlacks %q", r, want)
		}
	}

	r = matches(map[string]any{"name": "Flat"})
	if !strings.Contains(r, "Latest alerts for Flat") || strings.Contains(r, "Octavia") || strings.Contains(r, "— Flat") {
		t.Errorf("one search: %q", r)
	}
	if r := matches(map[string]any{"count": 1.0}); strings.Contains(r, "ad 2") {
		t.Errorf("count 1: %q", r)
	}
	if r := matches(map[string]any{"name": "nope"}); r != "❌ No such search." {
		t.Errorf("unknown: %q", r)
	}

	// Long lists stay under Discord's limit.
	s.matches = nil
	for range 25 {
		s.matches = append(s.matches, subscriptions.MatchRef{SearchID: 1, SearchName: strings.Repeat("x", 80), Source: "ss.com", ExternalID: "1", At: now})
	}
	if r := matches(map[string]any{"count": 25.0}); len([]rune(r)) > 2000 || !strings.Contains(r, "more; ask for fewer") {
		t.Errorf("long list: %d runes, %q", len([]rune(r)), r)
	}
}

func TestCompleteOwnSearches(t *testing.T) {
	b, _, _ := newBot()
	ctx := context.Background()
	b.Run(ctx, alice, Call{Command: CmdWatch, Sub: "cars", Args: map[string]any{"name": "Skoda"}})
	b.Run(ctx, alice, Call{Command: CmdWatch, Sub: "flats", Args: map[string]any{"name": "Flat", "region": "Rīga", "deal": "Izīrē"}})

	if got := b.Complete(ctx, alice, Call{Command: CmdEdit, Sub: "cars", Args: map[string]any{"name": ""}}, "name"); !reflect.DeepEqual(got, []string{"Skoda"}) {
		t.Errorf("edit cars names = %v", got)
	}
	if got := b.Complete(ctx, alice, Call{Command: CmdMatches, Args: map[string]any{"name": ""}}, "name"); len(got) != 2 {
		t.Errorf("matches names = %v", got)
	}
	got := b.Complete(ctx, alice, Call{Command: CmdEdit, Sub: "cars", Args: map[string]any{"clear": "model, pri"}}, "clear")
	if !slices.Contains(got, "model, price_max") || !slices.Contains(got, "model, price") || slices.Contains(got, "model, model") {
		t.Errorf("clear = %v", got)
	}
}

func TestSetup(t *testing.T) {
	b, m, s := newBot()
	ctx := context.Background()
	setup := func(sub string, args map[string]any, postable ...string) string {
		c := Call{Command: CmdSetup, Sub: sub, Args: args, Postable: map[string]bool{}}
		for _, p := range postable {
			c.Postable[p] = true
		}
		return b.Run(ctx, alice, c).Text
	}

	var cmd Command
	for _, c := range b.Commands() {
		if c.Name == CmdSetup {
			cmd = c
		}
	}
	if !cmd.ManagersOnly || len(cmd.Subcommands) != 3 {
		t.Errorf("setup = %+v", cmd)
	}

	// Only admins.
	if r := setup(SetupStatusChannel, map[string]any{"channel": "status"}, "status"); !strings.Contains(r, "Only the bot's admins") || s.status != nil {
		t.Errorf("non-admin: %q", r)
	}
	s.admin = true

	if r := setup(SetupShow, map[string]any{}); !strings.Contains(r, "ADMIN_CHANNEL_ID from .env applies") {
		t.Errorf("show unset: %q", r)
	}
	if r := setup(SetupStatusChannel, map[string]any{"channel": "status"}); !strings.Contains(r, "you can't post in #status") || s.status != nil {
		t.Errorf("unpostable: %q", r)
	}
	m.failSend = true
	if r := setup(SetupStatusChannel, map[string]any{"channel": "status"}, "status"); !strings.Contains(r, "I can't post in #status") || s.status != nil {
		t.Errorf("bot can't post: %q", r)
	}
	m.failSend = false
	if r := setup(SetupStatusChannel, map[string]any{"channel": "status"}, "status"); !strings.Contains(r, "now go to #status") || *s.status != (events.Channel{Kind: "chat", Target: "status"}) {
		t.Errorf("set: %q, %+v", r, s.status)
	}
	if len(m.sent) != 1 || !strings.HasPrefix(m.sent[0], "status: ✅") {
		t.Errorf("test message = %v", m.sent)
	}
	if r := setup(SetupShow, map[string]any{}); !strings.Contains(r, "Status messages: #status") {
		t.Errorf("show set: %q", r)
	}
	if r := setup(SetupStatusOff, map[string]any{}); !strings.Contains(r, "off") || s.status.Target != "" {
		t.Errorf("off: %q, %+v", r, s.status)
	}
	if r := setup(SetupShow, map[string]any{}); !strings.Contains(r, "Status messages: off") {
		t.Errorf("show off: %q", r)
	}
}
