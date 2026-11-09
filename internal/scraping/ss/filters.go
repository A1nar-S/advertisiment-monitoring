package ss

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
)

// Category IDs users pick from; each has a codebook.
const (
	CategoryCars  = "cars"
	CategoryFlats = "flats"
)

// CompileSearch implements sources.Catalog. Searches are written in the
// terms of ss.com's own search forms and checked against the codebooks.
//
// Cars: every filter runs locally on the one shared transport/cars feed.
//
//	make: [Skoda, Toyota]     model: [Octavia, "Passat*"]     fuel: [Dīzelis]
//	gearbox: [Automāts]       year: {min: 2012}               equipment: [Kondicionieris]
//	region: [Rīga, Rīgas rajons]   (read from the ad page, like gearbox)
//
// Flats: region, district and deal choose the feeds; the rest are filters.
//
//	region: [Rīga]   district: [Centrs, Teika]   deal: [Izīrē]   rooms: {min: 2}   series: [Jaun.]
//
// Names ignore case and diacritics and come back spelled as on ss.com.
func (*Client) CompileSearch(category string, filters map[string]any) (sources.Compiled, error) {
	var c sources.Compiled
	var err error
	switch category {
	case CategoryCars:
		var cb *Codebook
		if cb, err = CarsCodebook(); err != nil {
			return c, err
		}
		c.Feeds = []string{CarsCategory}
		c.Filters, c.Spec, err = cb.normalize(filters)
	case CategoryFlats:
		var cb *Codebook
		if cb, err = FlatsCodebook(); err != nil {
			return c, err
		}
		c.Feeds, c.Filters, c.Spec, err = cb.flatSearch(filters)
	default:
		return c, &sources.InvalidSearch{Msg: fmt.Sprintf("unknown category %q; choose %s or %s", category, CategoryCars, CategoryFlats)}
	}
	if err != nil {
		var inv *sources.InvalidSearch
		if !errors.As(err, &inv) {
			err = &sources.InvalidSearch{Msg: err.Error()}
		}
		return sources.Compiled{}, err
	}
	return c, nil
}

// flatSearch turns region/district/deal into feeds and checks the rest.
func (cb *Codebook) flatSearch(in map[string]any) (feeds []string, filters, spec map[string]any, err error) {
	regionNames, err := stringList("region", in["region"])
	if err != nil {
		return nil, nil, nil, err
	}
	districtNames, err := stringList("district", in["district"])
	if err != nil {
		return nil, nil, nil, err
	}
	dealNames, err := stringList("deal", in["deal"])
	if err != nil {
		return nil, nil, nil, err
	}
	dealField, _ := cb.Field("deal")
	if len(regionNames) == 0 {
		return nil, nil, nil, errors.New("choose a region, e.g. Rīga")
	}
	if len(dealNames) == 0 {
		return nil, nil, nil, fmt.Errorf("choose a deal: %s (Izīrē = for rent)", strings.Join(dealField.optionNames(), ", "))
	}

	var regions []*Region
	var shownRegions, shownDistricts, shownDeals []string
	for _, n := range regionNames {
		r, ok := cb.Region(n)
		if !ok {
			return nil, nil, nil, fmt.Errorf("unknown region %q%s", n, didYouMean(n, cb.regionNames()))
		}
		regions = append(regions, r)
		shownRegions = append(shownRegions, r.Name)
	}
	var deals []string
	for _, n := range dealNames {
		o, err := dealField.lookup(n)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("deal: %w", err)
		}
		deals = append(deals, o.ID)
		shownDeals = append(shownDeals, o.Name)
	}

	// Each district goes with the region that has it; a region given no
	// district is watched whole.
	areas := map[*Region][]string{}
	for _, n := range districtNames {
		found := false
		var all []string
		for _, r := range regions {
			if d, ok := r.District(n); ok {
				areas[r] = append(areas[r], d.ID)
				shownDistricts = append(shownDistricts, d.Name)
				found = true
				break
			}
			for _, d := range r.Districts {
				all = append(all, d.Name)
			}
		}
		if !found {
			return nil, nil, nil, fmt.Errorf("unknown district %q in %s%s", n, strings.Join(shownRegions, ", "), didYouMean(n, all))
		}
	}
	for _, r := range regions {
		ds := areas[r]
		if len(ds) == 0 {
			ds = []string{"all"}
		}
		for _, d := range ds {
			for _, deal := range deals {
				feeds = append(feeds, FlatsCategory+"/"+r.Slug+"/"+d+"/"+deal)
			}
		}
	}

	rest := map[string]any{}
	for k, v := range in {
		if k != "region" && k != "district" && k != "deal" {
			rest[k] = v
		}
	}
	filters, spec, err = cb.normalize(rest)
	if err != nil {
		return nil, nil, nil, err
	}
	spec["region"], spec["deal"] = toAny(shownRegions), toAny(shownDeals)
	if len(shownDistricts) > 0 {
		spec["district"] = toAny(shownDistricts)
	}
	return feeds, filters, spec, nil
}

// normalize checks filters against the codebook. It returns what the matcher
// runs on (wildcards and aliases expanded) and what the user sees again
// (their own choices, canonically spelled).
func (cb *Codebook) normalize(in map[string]any) (compiled, shown map[string]any, err error) {
	compiled, shown = map[string]any{}, map[string]any{}
	var makes []*Make
	// Makes first: models are checked against them.
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if d := boolInt(b == "make") - boolInt(a == "make"); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})

	for _, key := range keys {
		v := in[key]
		if key == KeywordsKey && cb.Category == CarsCategory {
			words, err := keywords(v)
			if err != nil {
				return nil, nil, err
			}
			if len(words) > 0 {
				compiled[TextAttr] = map[string]any{"contains": toAny(words)}
				shown[key] = toAny(words)
			}
			continue
		}
		f, ok := cb.Field(key)
		if !ok || f.From == fromFeed {
			valid := cb.Keys()
			if cb.Category == CarsCategory {
				valid = append(valid, KeywordsKey)
			}
			return nil, nil, fmt.Errorf("unknown filter %q%s; valid: %s", key, didYouMean(key, valid), strings.Join(valid, ", "))
		}
		if f.Kind == kindRange {
			r, err := rangeOf(f, v)
			if err != nil {
				return nil, nil, err
			}
			if len(r) > 0 {
				compiled[key], shown[key] = r, r
			}
			continue
		}

		names, err := stringList(key, v)
		if err != nil {
			return nil, nil, err
		}
		if len(names) == 0 {
			continue // empty = any
		}
		var canon, show []string
		switch key {
		case "make":
			for _, n := range names {
				m, ok := cb.MakeByName(n)
				if !ok {
					return nil, nil, fmt.Errorf("unknown make %q%s", n, didYouMean(n, cb.makeNames()))
				}
				makes = append(makes, m)
				canon = append(canon, m.Name)
			}
			show = canon
		case "model":
			if canon, show, err = cb.models(names, makes); err != nil {
				return nil, nil, err
			}
		case "fuel":
			if canon, show, err = fuels(f, names); err != nil {
				return nil, nil, err
			}
		default:
			for _, n := range names {
				o, err := f.lookup(n)
				if err != nil {
					return nil, nil, fmt.Errorf("%s: %w", f.Label, err)
				}
				canon = append(canon, o.Name)
			}
			show = canon
		}
		canon, show = uniq(canon), uniq(show)
		shown[key] = toAny(show)
		if f.Kind == kindAll {
			compiled[key] = map[string]any{"all": toAny(canon)}
		} else {
			compiled[key] = toAny(canon)
		}
	}
	return compiled, shown, nil
}

// KeywordsKey is the car filter of free words, any of which the ad's text
// must contain ("quattro"). It compiles to a Contains filter on TextAttr.
const KeywordsKey = "keywords"

// TextAttr is the attr with an ad's full description, from the ad page.
const TextAttr = "text"

const (
	maxKeywords   = 10
	maxKeywordLen = 50
)

// keywords reads the keywords filter: trimmed, without repeats.
func keywords(v any) ([]string, error) {
	names, err := stringList(KeywordsKey, v)
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.Join(strings.Fields(n), " ")
		switch {
		case n == "" || seen[fold(n)]:
			continue
		case len([]rune(n)) < 2:
			return nil, fmt.Errorf("keyword %q is too short; use at least 2 letters", n)
		case len([]rune(n)) > maxKeywordLen:
			return nil, fmt.Errorf("keyword %q is too long; keep it under %d letters", n, maxKeywordLen)
		}
		seen[fold(n)] = true
		out = append(out, n)
	}
	if len(out) > maxKeywords {
		return nil, fmt.Errorf("at most %d keywords, got %d", maxKeywords, len(out))
	}
	return out, nil
}

// rangeOf reads {min, max}; either may be missing, both must be numbers.
func rangeOf(f *Field, v any) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: give a range {min, max}", f.Label)
	}
	out := map[string]any{}
	for k, b := range m {
		if k != "min" && k != "max" {
			return nil, fmt.Errorf("%s: a range has only min and max, not %q", f.Label, k)
		}
		if b == nil {
			continue
		}
		n, ok := number(b)
		if !ok {
			return nil, fmt.Errorf("%s: %s must be a number, got %v", f.Label, k, b)
		}
		out[k] = n
	}
	if lo, ok := out["min"].(float64); ok {
		if hi, ok := out["max"].(float64); ok && lo > hi {
			return nil, fmt.Errorf("%s: min %v is above max %v", f.Label, lo, hi)
		}
	}
	return out, nil
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

// lookup finds an option, explaining near misses and names that exist in
// several groups ("Apsildāmi" is both "Sēdekļi: Apsildāmi" and "Spoguļi: ...").
func (f *Field) lookup(name string) (Option, error) {
	if o, ok := f.Option(name); ok {
		return o, nil
	}
	var groups []string
	for _, o := range f.Options {
		if _, item, ok := strings.Cut(o.Name, ": "); ok && fold(item) == fold(name) {
			groups = append(groups, o.Name)
		}
	}
	if len(groups) > 0 {
		return Option{}, fmt.Errorf("%q is in several groups; write one of %s", name, quoteAll(groups))
	}
	return Option{}, fmt.Errorf("unknown value %q%s; valid: %s", name, didYouMean(name, f.optionNames()), strings.Join(f.optionNames(), ", "))
}

// models checks model names against the chosen makes (or every make when
// none is chosen). A make without a model list in the codebook accepts any.
// "Passat*" takes every generation: ss.com lists some models per generation
// ("Passat (B5)", "Golf 7"). shown keeps the wildcard as written.
func (cb *Codebook) models(names []string, makes []*Make) (compiled, shown []string, err error) {
	if len(makes) == 0 {
		for i := range cb.Makes {
			makes = append(makes, &cb.Makes[i])
		}
	}
	for _, n := range names {
		if prefix, ok := strings.CutSuffix(strings.TrimSpace(n), "*"); ok {
			before := len(compiled)
			for _, m := range makes {
				for _, o := range m.Models {
					if strings.HasPrefix(fold(o.Name), fold(prefix)) {
						compiled = append(compiled, o.Name)
					}
				}
			}
			if len(compiled) == before {
				return nil, nil, fmt.Errorf("no model of the chosen makes starts with %q", prefix)
			}
			shown = append(shown, prefix+"*")
			continue
		}
		found, open := false, false
		var all []string
		for _, m := range makes {
			if len(m.Models) == 0 {
				open = true
			}
			if o, ok := m.Model(n); ok {
				compiled, shown, found = append(compiled, o.Name), append(shown, o.Name), true
				break
			}
			for _, o := range m.Models {
				all = append(all, o.Name)
			}
		}
		switch {
		case found:
		case open && len(makes) < len(cb.Makes):
			compiled, shown = append(compiled, n), append(shown, n)
		default:
			return nil, nil, fmt.Errorf("unknown model %q for the chosen makes%s", n, didYouMean(n, all))
		}
	}
	return compiled, shown, nil
}

// fuels accepts the form's names and the list page's suffixes (D, H, E).
// Petrol/gas cannot be told from petrol on the list page, so asking for it
// also accepts plain petrol (in compiled, not in shown).
func fuels(f *Field, names []string) (compiled, shown []string, err error) {
	for _, n := range names {
		if canon, ok := fuelSuffix[strings.ToUpper(strings.TrimSpace(n))]; ok && n != "" {
			n = canon
		}
		o, err := f.lookup(n)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", f.Label, err)
		}
		compiled, shown = append(compiled, o.Name), append(shown, o.Name)
		if o.Name == "Benzīns/gāze" {
			compiled = append(compiled, "Benzīns")
		}
	}
	slices.Sort(compiled)
	return slices.Compact(compiled), shown, nil
}

// stringList reads a filter value written as one string or a list.
func stringList(key string, v any) ([]string, error) {
	switch t := v.(type) {
	case string:
		return []string{t}, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			out = append(out, fmt.Sprint(e))
		}
		return out, nil
	case nil:
		return nil, nil
	}
	return nil, fmt.Errorf("filter %q: expected a name or a list of names, got %v", key, v)
}

func (cb *Codebook) makeNames() []string {
	names := make([]string, len(cb.Makes))
	for i, m := range cb.Makes {
		names[i] = m.Name
	}
	return names
}

func didYouMean(s string, names []string) string {
	if c := closest(s, names); len(c) > 0 {
		return " (did you mean " + strings.Join(quoteAll(c), " or ") + "?)"
	}
	return ""
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// uniq drops repeats, keeping the first occurrence's place.
func uniq(ss []string) []string {
	seen := map[string]bool{}
	out := ss[:0:0]
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
