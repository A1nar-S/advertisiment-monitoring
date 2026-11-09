// Package web serves the dashboard: its JSON API (users' own searches,
// matches, the catalog to build search forms from, the admin overview) and
// the built single-page app. People log in with a one-time link from the
// chat bot; the session lives in an HttpOnly cookie.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/A1nar-S/advertisiment-monitoring/internal/listings"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions"
)

// Searches is what the dashboard needs from the subscriptions slice.
type Searches interface {
	Login(ctx context.Context, loginToken string) (string, subscriptions.Owner, error)
	SessionOwner(ctx context.Context, session string) (subscriptions.Owner, error)
	Logout(ctx context.Context, session string) error
	ListSearches(ctx context.Context, o subscriptions.Owner, all bool) ([]subscriptions.SearchView, error)
	CreateSearch(ctx context.Context, o subscriptions.Owner, in subscriptions.SearchInput) (subscriptions.SearchView, error)
	UpdateSearch(ctx context.Context, o subscriptions.Owner, id int64, in subscriptions.SearchInput) (subscriptions.SearchView, error)
	SetEnabled(ctx context.Context, o subscriptions.Owner, id int64, enabled bool) error
	DeleteSearch(ctx context.Context, o subscriptions.Owner, id int64) error
	Channels(ctx context.Context, o subscriptions.Owner) ([]subscriptions.ChannelView, error)
	RecentMatches(ctx context.Context, o subscriptions.Owner, searchID int64, limit int) ([]subscriptions.MatchRef, error)
	Categories() map[string][]sources.Category
}

// Listings looks up stored ads, to show matches.
type Listings interface {
	Summaries(ctx context.Context, source string, ids []string) (map[string]listings.Summary, error)
}

type Config struct {
	Searches    Searches
	Listings    Listings
	Overview    http.Handler // the admin overview (GET /api/overview)
	WebDir      string       // the built app; empty = API only
	PublicURL   string       // https:// makes the cookie Secure
	MaxSearches int
}

const (
	sessionCookie = "session"
	// csrfHeader must be on every request that changes something. A form or
	// a link on another site cannot set it; fetch() from our page does.
	csrfHeader = "X-Requested-With"
)

type server struct {
	Config
	secure bool
}

type ctxKey struct{}

// New returns the dashboard's handler.
func New(cfg Config) http.Handler {
	s := &server{Config: cfg, secure: strings.HasPrefix(cfg.PublicURL, "https://")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.Handle("GET /api/me", s.auth(s.me))
	mux.Handle("GET /api/categories", s.auth(s.categories))
	mux.Handle("GET /api/channels", s.auth(s.channels))
	mux.Handle("GET /api/searches", s.auth(s.listSearches))
	mux.Handle("POST /api/searches", s.auth(s.createSearch))
	mux.Handle("PUT /api/searches/{id}", s.auth(s.updateSearch))
	mux.Handle("PATCH /api/searches/{id}", s.auth(s.patchSearch))
	mux.Handle("DELETE /api/searches/{id}", s.auth(s.deleteSearch))
	mux.Handle("GET /api/matches", s.auth(s.matches))
	if cfg.Overview != nil {
		mux.Handle("GET /api/overview", s.auth(func(w http.ResponseWriter, r *http.Request, o subscriptions.Owner) {
			if !o.Admin {
				writeError(w, http.StatusForbidden, "admins only")
				return
			}
			cfg.Overview.ServeHTTP(w, r)
		}))
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) { writeError(w, http.StatusNotFound, "no such endpoint") })
	if cfg.WebDir != "" {
		mux.Handle("/", spa(cfg.WebDir))
	}
	return mux
}

// --- auth ---------------------------------------------------------------------

// login trades the bot's one-time link for a session cookie.
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	session, _, err := s.Searches.Login(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		if !errors.Is(err, subscriptions.ErrBadToken) {
			slog.Error("login", "err", err)
		}
		http.Redirect(w, r, "/?login=expired", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: session, Path: "/", HttpOnly: true, Secure: s.secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(subscriptions.SessionTTL / time.Second),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.Searches.Logout(r.Context(), c.Value); err != nil {
			slog.Error("logout", "err", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.secure, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

type ownerHandler func(http.ResponseWriter, *http.Request, subscriptions.Owner)

// auth resolves the session and, for anything but reads, checks the CSRF
// header.
func (s *server) auth(h ownerHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Header.Get(csrfHeader) == "" {
			writeError(w, http.StatusForbidden, "missing "+csrfHeader+" header")
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		o, err := s.Searches.SessionOwner(r.Context(), c.Value)
		if err != nil {
			if !errors.Is(err, subscriptions.ErrBadToken) {
				slog.Error("session", "err", err)
				writeError(w, http.StatusServiceUnavailable, "try again in a moment")
				return
			}
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		ctx, cancel := context.WithTimeout(context.WithValue(r.Context(), ctxKey{}, o), 10*time.Second)
		defer cancel()
		h(w, r.WithContext(ctx), o)
	})
}

// --- endpoints ------------------------------------------------------------------

func (s *server) me(w http.ResponseWriter, _ *http.Request, o subscriptions.Owner) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name": o.Name, "platform": o.Platform, "admin": o.Admin, "maxSearches": s.MaxSearches,
	})
}

func (s *server) categories(w http.ResponseWriter, _ *http.Request, _ subscriptions.Owner) {
	cats := s.Searches.Categories()[subscriptions.DefaultSource]
	if cats == nil {
		cats = []sources.Category{}
	}
	writeJSON(w, http.StatusOK, cats)
}

func (s *server) channels(w http.ResponseWriter, r *http.Request, o subscriptions.Owner) {
	chs, err := s.Searches.Channels(r.Context(), o)
	if s.fail(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, nonNil(chs))
}

func (s *server) listSearches(w http.ResponseWriter, r *http.Request, o subscriptions.Owner) {
	vs, err := s.Searches.ListSearches(r.Context(), o, r.URL.Query().Get("all") == "1")
	if s.fail(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, nonNil(vs))
}

type searchBody struct {
	Name      string         `json:"name"`
	Category  string         `json:"category"`
	Filters   map[string]any `json:"filters"`
	ChannelID int64          `json:"channelId"`
	Enabled   *bool          `json:"enabled"`
}

func (s *server) createSearch(w http.ResponseWriter, r *http.Request, o subscriptions.Owner) {
	b, ok := readBody(w, r)
	if !ok {
		return
	}
	v, err := s.Searches.CreateSearch(r.Context(), o, subscriptions.SearchInput{
		Name: b.Name, Category: b.Category, Filters: b.Filters, ChannelID: b.ChannelID, Origin: "web",
	})
	if s.fail(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *server) updateSearch(w http.ResponseWriter, r *http.Request, o subscriptions.Owner) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	b, ok := readBody(w, r)
	if !ok {
		return
	}
	v, err := s.Searches.UpdateSearch(r.Context(), o, id, subscriptions.SearchInput{
		Name: b.Name, Category: b.Category, Filters: b.Filters, ChannelID: b.ChannelID,
	})
	if s.fail(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// patchSearch pauses or resumes.
func (s *server) patchSearch(w http.ResponseWriter, r *http.Request, o subscriptions.Owner) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	b, ok := readBody(w, r)
	if !ok {
		return
	}
	if b.Enabled == nil {
		writeError(w, http.StatusUnprocessableEntity, "only enabled can be patched")
		return
	}
	if s.fail(w, s.Searches.SetEnabled(r.Context(), o, id, *b.Enabled)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteSearch(w http.ResponseWriter, r *http.Request, o subscriptions.Owner) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if s.fail(w, s.Searches.DeleteSearch(r.Context(), o, id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type matchView struct {
	subscriptions.MatchRef
	Listing *listings.Summary `json:"listing,omitempty"` // nil once purged
}

// matches lists the owner's latest alerts with the ads they were about.
func (s *server) matches(w http.ResponseWriter, r *http.Request, o subscriptions.Owner) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	refs, err := s.Searches.RecentMatches(r.Context(), o, 0, limit)
	if s.fail(w, err) {
		return
	}
	ids := map[string][]string{}
	for _, m := range refs {
		ids[m.Source] = append(ids[m.Source], m.ExternalID)
	}
	found := map[string]map[string]listings.Summary{}
	for source, list := range ids {
		if found[source], err = s.Listings.Summaries(r.Context(), source, list); s.fail(w, err) {
			return
		}
	}
	out := make([]matchView, len(refs))
	for i, m := range refs {
		out[i] = matchView{MatchRef: m}
		if l, ok := found[m.Source][m.ExternalID]; ok {
			out[i].Listing = &l
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// --- helpers --------------------------------------------------------------------

// fail writes err as a response: a user's mistake is 422 with its message.
func (s *server) fail(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var inv *sources.InvalidSearch
	switch {
	case errors.As(err, &inv):
		writeError(w, http.StatusUnprocessableEntity, inv.Msg)
	case errors.Is(err, subscriptions.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such search")
	default:
		slog.Error("dashboard api", "err", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
	}
	return true
}

func readBody(w http.ResponseWriter, r *http.Request) (searchBody, bool) {
	var b searchBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		writeError(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return b, false
	}
	return b, true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such search")
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// spa serves the built app, falling back to index.html for its own routes.
func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		files.ServeHTTP(w, r)
	})
}
