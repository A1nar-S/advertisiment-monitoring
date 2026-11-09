package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/listings"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions"
)

// fakeSearches: session "s-alice" is Alice (user 1), "s-admin" an admin;
// search 7 belongs to Alice.
type fakeSearches struct {
	created []subscriptions.SearchInput
	enabled map[int64]bool
}

var (
	alice = subscriptions.Owner{UserID: 1, Platform: "discord", ExternalID: "111", Name: "Alice"}
	admin = subscriptions.Owner{UserID: 9, Platform: "discord", ExternalID: "999", Name: "Admin", Admin: true}
)

func (*fakeSearches) Login(_ context.Context, token string) (string, subscriptions.Owner, error) {
	if token == "good" {
		return "s-alice", alice, nil
	}
	return "", subscriptions.Owner{}, subscriptions.ErrBadToken
}
func (*fakeSearches) SessionOwner(_ context.Context, session string) (subscriptions.Owner, error) {
	switch session {
	case "s-alice":
		return alice, nil
	case "s-admin":
		return admin, nil
	}
	return subscriptions.Owner{}, subscriptions.ErrBadToken
}
func (*fakeSearches) Logout(context.Context, string) error { return nil }
func (*fakeSearches) Preview(subscriptions.SearchInput, []events.ListingDiscovered) (subscriptions.Preview, error) {
	return subscriptions.Preview{}, nil
}
func (*fakeSearches) ListSearches(_ context.Context, o subscriptions.Owner, all bool) ([]subscriptions.SearchView, error) {
	if o.UserID != alice.UserID && (!all || !o.Admin) {
		return nil, nil
	}
	return []subscriptions.SearchView{{ID: 7, UserID: 1, Name: "cars"}}, nil
}
func (f *fakeSearches) CreateSearch(_ context.Context, _ subscriptions.Owner, in subscriptions.SearchInput) (subscriptions.SearchView, error) {
	if in.Name == "" {
		return subscriptions.SearchView{}, &sources.InvalidSearch{Msg: "give the search a name"}
	}
	f.created = append(f.created, in)
	return subscriptions.SearchView{ID: 8, Name: in.Name, Category: in.Category, Filters: in.Filters}, nil
}
func (*fakeSearches) UpdateSearch(_ context.Context, o subscriptions.Owner, id int64, in subscriptions.SearchInput) (subscriptions.SearchView, error) {
	if id != 7 || (o.UserID != alice.UserID && !o.Admin) {
		return subscriptions.SearchView{}, subscriptions.ErrNotFound
	}
	return subscriptions.SearchView{ID: 7, Name: in.Name}, nil
}
func (f *fakeSearches) SetEnabled(_ context.Context, o subscriptions.Owner, id int64, enabled bool) error {
	if id != 7 || o.UserID != alice.UserID {
		return subscriptions.ErrNotFound
	}
	f.enabled[id] = enabled
	return nil
}
func (*fakeSearches) DeleteSearch(_ context.Context, o subscriptions.Owner, id int64) error {
	if id != 7 || o.UserID != alice.UserID {
		return subscriptions.ErrNotFound
	}
	return nil
}
func (*fakeSearches) Channels(context.Context, subscriptions.Owner) ([]subscriptions.ChannelView, error) {
	return []subscriptions.ChannelView{{ID: 3, Kind: "discord", Label: "Private messages (discord)"}}, nil
}
func (*fakeSearches) RecentMatches(context.Context, subscriptions.Owner, int64, int) ([]subscriptions.MatchRef, error) {
	return []subscriptions.MatchRef{{SearchID: 7, Source: "ss.com", ExternalID: "42"}, {SearchID: 7, Source: "ss.com", ExternalID: "gone"}}, nil
}
func (*fakeSearches) Categories() map[string][]sources.Category {
	return map[string][]sources.Category{"ss.com": {{ID: "cars", Label: "Cars"}}}
}

type fakeListings struct{}

func (fakeListings) Recent(context.Context, string, int32) ([]listings.Stored, error) {
	return nil, nil
}
func (fakeListings) Get(context.Context, int64) (listings.Stored, error) {
	return listings.Stored{}, listings.ErrNotFound
}
func (fakeListings) SaveDetails(context.Context, events.ListingDiscovered, events.ListingDiscovered) error {
	return nil
}
func (fakeListings) MarkGone(context.Context, string, string) error { return nil }

func (fakeListings) Summaries(_ context.Context, _ string, ids []string) (map[string]listings.Summary, error) {
	out := map[string]listings.Summary{}
	for _, id := range ids {
		if id == "42" {
			out[id] = listings.Summary{ExternalID: id, Title: "Skoda"}
		}
	}
	return out, nil
}

func newServer(t *testing.T) (http.Handler, *fakeSearches) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakeSearches{enabled: map[int64]bool{}}
	overview := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"stats":{}}`) })
	return New(Config{Searches: f, Listings: fakeListings{}, Overview: overview, WebDir: dir, PublicURL: "https://ads.example", MaxSearches: 10}), f
}

func do(h http.Handler, method, path, session, body string, csrf bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if session != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	}
	if csrf {
		r.Header.Set(csrfHeader, "fetch")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLoginFlow(t *testing.T) {
	h, _ := newServer(t)
	w := do(h, "GET", "/api/login?token=good", "", "", false)
	c := w.Result().Cookies()
	if w.Code != http.StatusSeeOther || len(c) != 1 || c[0].Value != "s-alice" || !c[0].HttpOnly || !c[0].Secure || c[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("login: %d %+v", w.Code, c)
	}
	if w := do(h, "GET", "/api/login?token=used", "", "", false); w.Header().Get("Location") != "/?login=expired" {
		t.Errorf("bad token redirect = %q", w.Header().Get("Location"))
	}
	if w := do(h, "GET", "/api/me", "", "", false); w.Code != http.StatusUnauthorized {
		t.Errorf("me without session: %d", w.Code)
	}
	w = do(h, "GET", "/api/me", "s-alice", "", false)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"Alice"`) || !strings.Contains(w.Body.String(), `"maxSearches":10`) {
		t.Errorf("me: %d %s", w.Code, w.Body)
	}
}

func TestSearchEndpoints(t *testing.T) {
	h, f := newServer(t)

	body := `{"name":"Parents","category":"cars","filters":{"make":["Skoda"]},"channelId":3}`
	if w := do(h, "POST", "/api/searches", "s-alice", body, false); w.Code != http.StatusForbidden {
		t.Errorf("POST without the CSRF header: %d, want 403", w.Code)
	}
	w := do(h, "POST", "/api/searches", "s-alice", body, true)
	if w.Code != http.StatusCreated || len(f.created) != 1 || f.created[0].Origin != "web" || f.created[0].ChannelID != 3 {
		t.Fatalf("create: %d %s %+v", w.Code, w.Body, f.created)
	}
	w = do(h, "POST", "/api/searches", "s-alice", `{"category":"cars"}`, true)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "give the search a name") {
		t.Errorf("invalid: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/api/searches", "s-alice", `{"title":"unknown field"}`, true); w.Code != http.StatusBadRequest {
		t.Errorf("unknown JSON field: %d", w.Code)
	}

	if w := do(h, "PATCH", "/api/searches/7", "s-alice", `{"enabled":false}`, true); w.Code != http.StatusNoContent || f.enabled[7] {
		t.Errorf("pause: %d", w.Code)
	}
	if w := do(h, "PUT", "/api/searches/7", "s-alice", `{"name":"x","category":"cars"}`, true); w.Code != http.StatusOK {
		t.Errorf("update: %d %s", w.Code, w.Body)
	}
	if w := do(h, "DELETE", "/api/searches/8", "s-alice", "", true); w.Code != http.StatusNotFound {
		t.Errorf("delete someone else's: %d, want 404", w.Code)
	}
	if w := do(h, "DELETE", "/api/searches/7", "s-alice", "", true); w.Code != http.StatusNoContent {
		t.Errorf("delete: %d", w.Code)
	}

	var cats []sources.Category
	w = do(h, "GET", "/api/categories", "s-alice", "", false)
	if err := json.Unmarshal(w.Body.Bytes(), &cats); err != nil || len(cats) != 1 || cats[0].ID != "cars" {
		t.Errorf("categories: %s", w.Body)
	}
	w = do(h, "GET", "/api/matches", "s-alice", "", false)
	if !strings.Contains(w.Body.String(), `"title":"Skoda"`) || strings.Count(w.Body.String(), `"listing"`) != 1 {
		t.Errorf("matches (one purged) = %s", w.Body)
	}
}

func TestAdminOnlyAndSPA(t *testing.T) {
	h, _ := newServer(t)
	if w := do(h, "GET", "/api/overview", "s-alice", "", false); w.Code != http.StatusForbidden {
		t.Errorf("overview for a user: %d, want 403", w.Code)
	}
	if w := do(h, "GET", "/api/overview", "s-admin", "", false); w.Code != http.StatusOK {
		t.Errorf("overview for the admin: %d", w.Code)
	}
	if w := do(h, "GET", "/api/searches?all=1", "s-admin", "", false); !strings.Contains(w.Body.String(), `"name":"cars"`) {
		t.Errorf("admin all searches: %s", w.Body)
	}
	if w := do(h, "GET", "/searches/7", "", "", false); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "app") {
		t.Errorf("SPA fallback: %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/api/nope", "s-alice", "", false); w.Code != http.StatusNotFound || !strings.Contains(w.Header().Get("Content-Type"), "json") {
		t.Errorf("unknown API path: %d", w.Code)
	}
}
