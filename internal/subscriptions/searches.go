package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions/subscriptionsdb"
)

// DefaultSource is the source a search watches unless it says otherwise.
const DefaultSource = "ss.com"

// ErrNotFound: no such search, or not yours. The two look the same so ids
// of other users' searches leak nothing.
var ErrNotFound = errors.New("search not found")

// Owner is who is acting: a user, possibly an admin. Platform and
// ExternalID are their chat account ("discord", "123...").
type Owner struct {
	UserID     int64
	Platform   string
	ExternalID string
	Name       string
	Admin      bool
}

func (o Owner) can(userID int64) bool { return o.Admin || o.UserID == userID }

// SearchInput is a search as a user writes it: a category and form-style
// filters in the source's own names ({"make": ["Skoda"], "price": {"max": 7000}}).
type SearchInput struct {
	Name      string
	Source    string // default DefaultSource
	Category  string // e.g. "cars", "flats"
	Filters   map[string]any
	ChannelID int64  // where alerts go; 0 on update keeps the current one
	Origin    string // "discord", "telegram", "web"
}

// SearchView is a saved search as shown to its owner.
type SearchView struct {
	ID        int64          `json:"id"`
	UserID    int64          `json:"userId"`
	UserName  string         `json:"userName"`
	Name      string         `json:"name"`
	Enabled   bool           `json:"enabled"`
	Source    string         `json:"source"`
	Category  string         `json:"category"`
	Filters   map[string]any `json:"filters"` // as the user wrote them, canonically spelled
	Feeds     []string       `json:"feeds"`
	Channel   ChannelView    `json:"channel"`
	Matches   int64          `json:"matches"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type ChannelView struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Label  string `json:"label"` // "Private messages", "#cars"
}

// spec is what the searches.spec column holds.
type spec struct {
	Category string         `json:"category"`
	Filters  map[string]any `json:"filters"`
}

// WithLimits sets how many searches one user may have (0 = unlimited) and
// which chat accounts are admins, written "platform:id" ("discord:123").
func (s *Service) WithLimits(maxSearches int, admins []string) *Service {
	s.maxSearches = maxSearches
	s.admins = map[string]bool{}
	for i, a := range admins {
		key, ok := adminKey(a)
		if !ok {
			// Don't log the entry itself: it comes straight from .env.
			slog.Warn("ADMIN_USERS entry ignored: write it as platform:id, e.g. discord:123456789012345678",
				"position", i+1)
			continue
		}
		s.admins[key] = true
	}
	return s
}

// adminKey normalizes one ADMIN_USERS entry to "platform:id". It forgives
// stray quotes, spaces and a capitalized platform ("Discord:123"), but a
// bare ID is rejected: it can't say which chat platform it belongs to.
func adminKey(entry string) (string, bool) {
	entry = strings.Trim(strings.TrimSpace(entry), `"'`)
	platform, id, ok := strings.Cut(entry, ":")
	platform, id = strings.ToLower(strings.TrimSpace(platform)), strings.TrimSpace(id)
	if !ok || platform == "" || id == "" {
		return "", false
	}
	return platform + ":" + id, true
}

// PlatformOwner returns the user for a chat account, creating it on first
// contact.
func (s *Service) PlatformOwner(ctx context.Context, platform, externalID, displayName string) (Owner, error) {
	id, err := s.q.UpsertPlatformUser(ctx, subscriptionsdb.UpsertPlatformUserParams{
		Platform: platform, ExternalID: externalID, DisplayName: displayName,
	})
	if err != nil {
		return Owner{}, fmt.Errorf("user: %w", err)
	}
	return s.owner(id, platform, externalID, displayName), nil
}

func (s *Service) owner(id int64, platform, externalID, name string) Owner {
	return Owner{UserID: id, Platform: platform, ExternalID: externalID, Name: name, Admin: s.admins[platform+":"+externalID]}
}

// EnsureChannel registers a delivery target of the owner (a private chat or
// a group channel) and returns it. An empty label keeps the stored one.
func (s *Service) EnsureChannel(ctx context.Context, o Owner, kind, target, label string) (ChannelView, error) {
	id, err := s.q.UpsertChannel(ctx, subscriptionsdb.UpsertChannelParams{UserID: o.UserID, Kind: kind, Target: target, Label: label})
	if err != nil {
		return ChannelView{}, fmt.Errorf("channel: %w", err)
	}
	return ChannelView{ID: id, Kind: kind, Target: target, Label: label}, nil
}

// Channels lists where the owner's alerts can go.
func (s *Service) Channels(ctx context.Context, o Owner) ([]ChannelView, error) {
	rows, err := s.q.ListUserChannels(ctx, o.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelView, len(rows))
	for i, r := range rows {
		out[i] = ChannelView{ID: r.ID, Kind: r.Kind, Target: r.Target, Label: r.Label}
	}
	return out, nil
}

// CreateSearch validates and saves a new search.
func (s *Service) CreateSearch(ctx context.Context, o Owner, in SearchInput) (SearchView, error) {
	if s.maxSearches > 0 && !o.Admin {
		n, err := s.q.CountUserSearches(ctx, o.UserID)
		if err != nil {
			return SearchView{}, err
		}
		if n >= int64(s.maxSearches) {
			return SearchView{}, &sources.InvalidSearch{Msg: fmt.Sprintf("you already have %d searches, the limit; delete one first", n)}
		}
	}
	if in.ChannelID == 0 {
		return SearchView{}, &sources.InvalidSearch{Msg: "choose where alerts should go"}
	}
	c, err := s.compile(ctx, o, in)
	if err != nil {
		return SearchView{}, err
	}
	id, err := s.q.InsertSearch(ctx, subscriptionsdb.InsertSearchParams{
		UserID: o.UserID, ChannelID: in.ChannelID, Name: in.Name, Source: c.source,
		Feeds: c.feeds, Filters: c.filters, Spec: c.spec, Origin: in.Origin,
	})
	if err != nil {
		return SearchView{}, nameTaken(err, in.Name)
	}
	return s.GetSearch(ctx, o, id)
}

// UpdateSearch replaces a search's name, filters and (if given) channel.
func (s *Service) UpdateSearch(ctx context.Context, o Owner, id int64, in SearchInput) (SearchView, error) {
	cur, err := s.GetSearch(ctx, o, id)
	if err != nil {
		return SearchView{}, err
	}
	if in.ChannelID == 0 {
		in.ChannelID = cur.Channel.ID
	}
	c, err := s.compile(ctx, Owner{UserID: cur.UserID, Admin: o.Admin}, in)
	if err != nil {
		return SearchView{}, err
	}
	_, err = s.q.UpdateSearch(ctx, subscriptionsdb.UpdateSearchParams{
		ID: id, ChannelID: in.ChannelID, Name: in.Name, Source: c.source,
		Feeds: c.feeds, Filters: c.filters, Spec: c.spec,
	})
	if err != nil {
		return SearchView{}, nameTaken(err, in.Name)
	}
	return s.GetSearch(ctx, o, id)
}

// SaveSearchByName creates the owner's search called in.Name, or replaces it
// if it exists: how the bot edits ("/watch" again with the same name).
func (s *Service) SaveSearchByName(ctx context.Context, o Owner, in SearchInput) (v SearchView, created bool, err error) {
	id, err := s.q.GetSearchIDByName(ctx, subscriptionsdb.GetSearchIDByNameParams{UserID: o.UserID, Name: in.Name})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		v, err = s.CreateSearch(ctx, o, in)
		return v, err == nil, err
	case err != nil:
		return v, false, err
	}
	v, err = s.UpdateSearch(ctx, o, id, in)
	return v, false, err
}

// FindSearch looks up one of the owner's searches by name.
func (s *Service) FindSearch(ctx context.Context, o Owner, name string) (SearchView, error) {
	id, err := s.q.GetSearchIDByName(ctx, subscriptionsdb.GetSearchIDByNameParams{UserID: o.UserID, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return SearchView{}, ErrNotFound
	}
	if err != nil {
		return SearchView{}, err
	}
	return s.GetSearch(ctx, o, id)
}

// GetSearch returns one search the owner may see.
func (s *Service) GetSearch(ctx context.Context, o Owner, id int64) (SearchView, error) {
	r, err := s.q.GetSearchView(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !o.can(r.UserID)) {
		return SearchView{}, ErrNotFound
	}
	if err != nil {
		return SearchView{}, err
	}
	return viewOf(subscriptionsdb.ListSearchViewsRow(r))
}

// ListSearches returns the owner's searches; everyone's for an admin who
// asks for all.
func (s *Service) ListSearches(ctx context.Context, o Owner, all bool) ([]SearchView, error) {
	rows, err := s.q.ListSearchViews(ctx, subscriptionsdb.ListSearchViewsParams{Everyone: all && o.Admin, UserID: o.UserID})
	if err != nil {
		return nil, err
	}
	out := make([]SearchView, 0, len(rows))
	for _, r := range rows {
		v, err := viewOf(r)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// SetEnabled pauses or resumes a search.
func (s *Service) SetEnabled(ctx context.Context, o Owner, id int64, enabled bool) error {
	if _, err := s.GetSearch(ctx, o, id); err != nil {
		return err
	}
	_, err := s.q.SetSearchEnabled(ctx, subscriptionsdb.SetSearchEnabledParams{ID: id, Enabled: enabled})
	return err
}

// DeleteSearch removes a search and its match history.
func (s *Service) DeleteSearch(ctx context.Context, o Owner, id int64) error {
	if _, err := s.GetSearch(ctx, o, id); err != nil {
		return err
	}
	_, err := s.q.DeleteSearch(ctx, id)
	return err
}

// MatchRef is one ad a user's search was told about.
type MatchRef struct {
	SearchID   int64     `json:"searchId"`
	SearchName string    `json:"searchName"`
	Source     string    `json:"source"`
	ExternalID string    `json:"externalId"`
	At         time.Time `json:"at"`
}

// RecentMatches lists the owner's latest matches, newest first: of one
// search, or of all of them when searchID is 0.
func (s *Service) RecentMatches(ctx context.Context, o Owner, searchID int64, limit int) ([]MatchRef, error) {
	p := subscriptionsdb.ListUserMatchesParams{UserID: o.UserID, MaxRows: int32(limit)}
	if searchID != 0 {
		p.SearchID = &searchID
	}
	rows, err := s.q.ListUserMatches(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make([]MatchRef, len(rows))
	for i, r := range rows {
		out[i] = MatchRef{SearchID: r.SearchID, SearchName: r.SearchName, Source: r.Source, ExternalID: r.ExternalID, At: r.CreatedAt.Time}
	}
	return out, nil
}

// Categories lists what the registered sources can search, per source.
func (s *Service) Categories() map[string][]sources.Category {
	out := map[string][]sources.Category{}
	for _, name := range s.sources.Names() {
		if c, ok := s.sources[name].(sources.Catalog); ok {
			out[name] = c.Categories()
		}
	}
	return out
}

type compiled struct {
	source        string
	feeds         []string
	filters, spec []byte
}

// compile validates a search through its source and the filter parser: the
// one path every search takes, from the bot or the dashboard.
func (s *Service) compile(ctx context.Context, o Owner, in SearchInput) (compiled, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 80 {
		return compiled{}, &sources.InvalidSearch{Msg: "give the search a name (up to 80 characters)"}
	}
	if _, err := s.q.GetUserChannel(ctx, subscriptionsdb.GetUserChannelParams{ID: in.ChannelID, UserID: o.UserID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return compiled{}, &sources.InvalidSearch{Msg: "unknown alert channel"}
		}
		return compiled{}, err
	}
	source := in.Source
	if source == "" {
		source = DefaultSource
	}
	src, err := s.sources.Get(source)
	if err != nil {
		return compiled{}, &sources.InvalidSearch{Msg: err.Error()}
	}
	cat, ok := src.(sources.Catalog)
	if !ok {
		return compiled{}, &sources.InvalidSearch{Msg: source + " has no searchable categories"}
	}
	c, err := cat.CompileSearch(in.Category, in.Filters)
	if err != nil {
		return compiled{}, err
	}
	filters, err := parseFilters(c.Filters)
	if err != nil {
		return compiled{}, &sources.InvalidSearch{Msg: err.Error()}
	}
	fj, err := json.Marshal(filters)
	if err != nil {
		return compiled{}, err
	}
	sj, err := json.Marshal(spec{Category: in.Category, Filters: c.Spec})
	if err != nil {
		return compiled{}, err
	}
	return compiled{source: source, feeds: c.Feeds, filters: fj, spec: sj}, nil
}

func viewOf(r subscriptionsdb.ListSearchViewsRow) (SearchView, error) {
	var sp spec
	if len(r.Spec) > 0 {
		if err := json.Unmarshal(r.Spec, &sp); err != nil {
			return SearchView{}, fmt.Errorf("search %d spec: %w", r.ID, err)
		}
	}
	if sp.Filters == nil {
		sp.Filters = map[string]any{}
	}
	return SearchView{
		ID: r.ID, UserID: r.UserID, UserName: r.UserName, Name: r.Name, Enabled: r.Enabled, Source: r.Source,
		Category: sp.Category, Filters: sp.Filters, Feeds: r.Feeds,
		Channel: ChannelView{ID: r.ChannelID, Kind: r.ChannelKind, Target: r.ChannelTarget, Label: r.ChannelLabel},
		Matches: r.Matches, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
	}, nil
}

func nameTaken(err error, name string) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return &sources.InvalidSearch{Msg: fmt.Sprintf("you already have a search named %q", name)}
	}
	return err
}

// parseFilters reads a compiled filters map: a mapping with min/max is a
// numeric range, {all: [...]} a list attr that must hold every value, and a
// list a set of allowed strings.
func parseFilters(in map[string]any) (Filters, error) {
	var f Filters
	for key, v := range in {
		switch t := v.(type) {
		case map[string]any:
			if words, ok := t["contains"]; ok && len(t) == 1 {
				vals, ok := words.([]any)
				if !ok {
					return f, fmt.Errorf("filter %q: contains: expects a list", key)
				}
				if f.Contains == nil {
					f.Contains = map[string][]string{}
				}
				for _, e := range vals {
					f.Contains[key] = append(f.Contains[key], fmt.Sprint(e))
				}
				continue
			}
			if all, ok := t["all"]; ok && len(t) == 1 {
				vals, ok := all.([]any)
				if !ok {
					return f, fmt.Errorf("filter %q: all: expects a list", key)
				}
				if f.AllOf == nil {
					f.AllOf = map[string][]string{}
				}
				for _, e := range vals {
					f.AllOf[key] = append(f.AllOf[key], fmt.Sprint(e))
				}
				continue
			}
			var r Range
			for k, bound := range t {
				n, ok := toFloat(bound)
				if !ok || (k != "min" && k != "max") {
					return f, fmt.Errorf("filter %q: only numeric min/max allowed, got %s=%v", key, k, bound)
				}
				if k == "min" {
					r.Min = &n
				} else {
					r.Max = &n
				}
			}
			if f.Ranges == nil {
				f.Ranges = map[string]Range{}
			}
			f.Ranges[key] = r
		case []any:
			var vals []string
			for _, e := range t {
				vals = append(vals, fmt.Sprint(e))
			}
			if len(vals) == 0 {
				continue // an empty list means "any"
			}
			if f.OneOf == nil {
				f.OneOf = map[string][]string{}
			}
			f.OneOf[key] = vals
		default:
			return f, fmt.Errorf("filter %q: expected {min,max} or a list, got %T", key, v)
		}
	}
	return f, nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
