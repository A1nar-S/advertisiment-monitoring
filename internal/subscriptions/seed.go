package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	"github.com/jackc/pgx/v5"

	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions/subscriptionsdb"
)

// File is the seed file format (see config/users.example.yaml). The file is
// the source of truth for what it lists; things it no longer lists are left in
// place, so retire a search with `enabled: false` rather than deleting it.
type File struct {
	Users []struct {
		Name     string `yaml:"name"`
		Channels []struct {
			Kind   string `yaml:"kind"`
			Target string `yaml:"target"`
		} `yaml:"channels"`
		Searches []struct {
			Name    string   `yaml:"name"`
			Channel string   `yaml:"channel"` // channel kind; defaults to the user's first
			Enabled *bool    `yaml:"enabled"` // defaults to true
			Source  string   `yaml:"source"`  // defaults to ss.com
			Feeds   []string `yaml:"feeds"`
			// Filters maps an attribute to {min, max} (numbers) or to a list of
			// allowed strings; "price" bounds the listing price.
			Filters map[string]any `yaml:"filters"`
		} `yaml:"searches"`
	} `yaml:"users"`
}

// SeedFromFile upserts the users, channels and searches in the YAML file at
// path, all or nothing.
func (s *Service) SeedFromFile(ctx context.Context, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	var f File
	if err := yaml.UnmarshalWithOptions(raw, &f, yaml.Strict()); err != nil {
		return fmt.Errorf("seed %s: %w", path, err)
	}

	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		for _, u := range f.Users {
			uid, err := q.UpsertUser(ctx, u.Name)
			if err != nil {
				return fmt.Errorf("user %s: %w", u.Name, err)
			}
			channels := map[string]int64{}
			var first int64
			for _, c := range u.Channels {
				id, err := q.UpsertChannel(ctx, subscriptionsdb.UpsertChannelParams{UserID: uid, Kind: c.Kind, Target: c.Target})
				if err != nil {
					return fmt.Errorf("user %s channel %s: %w", u.Name, c.Kind, err)
				}
				channels[c.Kind] = id
				if first == 0 {
					first = id
				}
			}

			for _, sr := range u.Searches {
				fail := func(format string, a ...any) error {
					return fmt.Errorf("user %s search %q: %s", u.Name, sr.Name, fmt.Sprintf(format, a...))
				}
				if len(sr.Feeds) == 0 {
					return fail("feeds is required")
				}
				source := orDefault(sr.Source, "ss.com")
				src, err := s.sources.Get(source)
				if err != nil {
					return fail("%v", err)
				}
				feeds, raw := sr.Feeds, sr.Filters
				if n, ok := src.(sources.SearchNormalizer); ok {
					if feeds, raw, err = n.NormalizeSearch(sr.Feeds, raw); err != nil {
						return fail("%v", err)
					}
				}
				cid := first
				if sr.Channel != "" {
					if cid = channels[sr.Channel]; cid == 0 {
						return fail("unknown channel %q", sr.Channel)
					}
				}
				if cid == 0 {
					return fail("user has no channels")
				}
				filters, err := parseFilters(raw)
				if err != nil {
					return fail("%v", err)
				}
				fj, _ := json.Marshal(filters)
				err = q.UpsertSearch(ctx, subscriptionsdb.UpsertSearchParams{
					UserID: uid, ChannelID: cid, Name: sr.Name, Enabled: sr.Enabled == nil || *sr.Enabled,
					Source: source, Feeds: feeds, Filters: fj,
				})
				if err != nil {
					return fail("%v", err)
				}
			}
		}
		return nil
	})
}

// ValidateFile checks a users file without a database: syntax, sources,
// channels and every search's filters (with the source's own vocabulary
// check). It reports every bad search, not just the first.
func ValidateFile(reg sources.Registry, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var f File
	if err := yaml.UnmarshalWithOptions(raw, &f, yaml.Strict()); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var errs []error
	for _, u := range f.Users {
		if len(u.Channels) == 0 {
			errs = append(errs, fmt.Errorf("user %s: no channels", u.Name))
		}
		for _, sr := range u.Searches {
			fail := func(err error) { errs = append(errs, fmt.Errorf("user %s search %q: %w", u.Name, sr.Name, err)) }
			if len(sr.Feeds) == 0 {
				fail(errors.New("feeds is required"))
				continue
			}
			src, err := reg.Get(orDefault(sr.Source, "ss.com"))
			if err != nil {
				fail(err)
				continue
			}
			filters := sr.Filters
			if n, ok := src.(sources.SearchNormalizer); ok {
				if _, filters, err = n.NormalizeSearch(sr.Feeds, filters); err != nil {
					fail(err)
					continue
				}
			}
			if _, err := parseFilters(filters); err != nil {
				fail(err)
			}
		}
	}
	return errors.Join(errs...)
}

// parseFilters reads the loosely typed YAML filters map: a mapping with
// min/max is a numeric range, {all: [...]} a list attr that must hold every
// value, and a list a set of allowed strings.
func parseFilters(in map[string]any) (Filters, error) {
	var f Filters
	for key, v := range in {
		switch t := v.(type) {
		case map[string]any:
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

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
