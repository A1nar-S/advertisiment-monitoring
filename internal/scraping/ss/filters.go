package ss

import (
	"fmt"
	"slices"
	"strings"
)

// NormalizeSearch implements sources.SearchNormalizer. Searches are written
// in the terms of ss.com's own search forms and checked against the codebooks.
//
// Cars (feeds: [transport/cars]): all filters run locally on the one shared
// feed.
//
//	make: [Skoda, Toyota]     model: [Octavia, "Passat*"]     fuel: [Dīzelis]
//	gearbox: Automāts         year: {min: 2012}               equipment: [Kondicionieris]
//
// Flats (feeds: [real-estate/flats]): region, district and deal choose the
// feeds; the rest are filters.
//
//	region: Rīga   district: [Centrs, Teika]   deal: Izīrē   rooms: {min: 2}   series: [Jaun.]
//
// Names ignore case and diacritics and come back spelled as on ss.com. Feeds
// given as full paths (real-estate/flats/riga/centre/hand_over) keep their
// filters as written.
func (*Client) NormalizeSearch(feeds []string, filters map[string]any) ([]string, map[string]any, error) {
	cars := 0
	for _, f := range feeds {
		if scope := parseFeed(f).scope; scope == CarsCategory || strings.HasPrefix(scope, CarsCategory+"/") {
			cars++
		}
	}
	switch {
	case cars > 0 && cars != len(feeds):
		return nil, nil, fmt.Errorf("a search cannot mix %s feeds with other categories", CarsCategory)
	case cars > 0:
		cb, err := CarsCodebook()
		if err != nil {
			return nil, nil, err
		}
		out, err := cb.normalize(filters)
		return feeds, out, err
	case len(feeds) == 1 && strings.Trim(feeds[0], "/") == FlatsCategory:
		cb, err := FlatsCodebook()
		if err != nil {
			return nil, nil, err
		}
		return cb.flatSearch(filters)
	}
	return feeds, filters, nil
}

// flatSearch turns region/district/deal into feeds and checks the rest.
func (cb *Codebook) flatSearch(in map[string]any) ([]string, map[string]any, error) {
	regionNames, err := stringList("region", in["region"])
	if err != nil {
		return nil, nil, err
	}
	districtNames, err := stringList("district", in["district"])
	if err != nil {
		return nil, nil, err
	}
	dealNames, err := stringList("deal", in["deal"])
	if err != nil {
		return nil, nil, err
	}
	dealField, _ := cb.Field("deal")
	if len(regionNames) == 0 {
		return nil, nil, fmt.Errorf("flats: region is required, e.g. region: Rīga")
	}
	if len(dealNames) == 0 {
		return nil, nil, fmt.Errorf("flats: deal is required: %s (Izīrē = for rent)", strings.Join(dealField.optionNames(), ", "))
	}

	var regions []*Region
	for _, n := range regionNames {
		r, ok := cb.Region(n)
		if !ok {
			return nil, nil, fmt.Errorf("unknown region %q%s", n, didYouMean(n, cb.regionNames()))
		}
		regions = append(regions, r)
	}
	var deals []string
	for _, n := range dealNames {
		o, err := dealField.lookup(n)
		if err != nil {
			return nil, nil, fmt.Errorf("filter \"deal\": %w", err)
		}
		deals = append(deals, o.ID)
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
				found = true
				break
			}
			for _, d := range r.Districts {
				all = append(all, d.Name)
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("unknown district %q in %s%s", n, strings.Join(regionNames, ", "), didYouMean(n, all))
		}
	}
	var feeds []string
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
	out, err := cb.normalize(rest)
	return feeds, out, err
}

func (cb *Codebook) normalize(in map[string]any) (map[string]any, error) {
	out := map[string]any{}
	var makes []*Make
	// Makes first: models are checked against them.
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return boolInt(b == "make") - boolInt(a == "make")
	})

	for _, key := range keys {
		v := in[key]
		f, ok := cb.Field(key)
		if !ok {
			return nil, fmt.Errorf("unknown filter %q%s; valid: %s", key, didYouMean(key, cb.Keys()), strings.Join(cb.Keys(), ", "))
		}
		if f.Kind == kindRange {
			if _, ok := v.(map[string]any); !ok {
				return nil, fmt.Errorf("filter %q: write a range as {min: ..., max: ...}", key)
			}
			out[key] = v
			continue
		}

		names, err := stringList(key, v)
		if err != nil {
			return nil, err
		}
		if len(names) == 0 {
			continue // empty list = any
		}
		var canon []string
		switch key {
		case "make":
			for _, n := range names {
				m, ok := cb.MakeByName(n)
				if !ok {
					return nil, fmt.Errorf("unknown make %q%s", n, didYouMean(n, cb.makeNames()))
				}
				makes = append(makes, m)
				canon = append(canon, m.Name)
			}
		case "model":
			if canon, err = cb.models(names, makes); err != nil {
				return nil, err
			}
		case "fuel":
			if canon, err = fuels(f, names); err != nil {
				return nil, err
			}
		default:
			for _, n := range names {
				o, err := f.lookup(n)
				if err != nil {
					return nil, fmt.Errorf("filter %q: %w", key, err)
				}
				canon = append(canon, o.Name)
			}
		}
		if f.Kind == kindAll {
			out[key] = map[string]any{"all": toAny(canon)}
		} else {
			out[key] = toAny(canon)
		}
	}
	return out, nil
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
func (cb *Codebook) models(names []string, makes []*Make) ([]string, error) {
	if len(makes) == 0 {
		for i := range cb.Makes {
			makes = append(makes, &cb.Makes[i])
		}
	}
	var out []string
	for _, n := range names {
		// "Passat*": ss.com lists some models per generation ("Passat (B5)",
		// "Golf 7"); a trailing * takes them all.
		if prefix, ok := strings.CutSuffix(n, "*"); ok {
			before := len(out)
			for _, m := range makes {
				for _, o := range m.Models {
					if strings.HasPrefix(fold(o.Name), fold(prefix)) {
						out = append(out, o.Name)
					}
				}
			}
			if len(out) == before {
				return nil, fmt.Errorf("no model of the chosen makes starts with %q", prefix)
			}
			continue
		}
		found, open := false, false
		var all []string
		for _, m := range makes {
			if len(m.Models) == 0 {
				open = true
			}
			if o, ok := m.Model(n); ok {
				out, found = append(out, o.Name), true
				break
			}
			for _, o := range m.Models {
				all = append(all, o.Name)
			}
		}
		switch {
		case found:
		case open && len(makes) < len(cb.Makes):
			out = append(out, n)
		default:
			return nil, fmt.Errorf("unknown model %q for the chosen makes%s", n, didYouMean(n, all))
		}
	}
	return out, nil
}

// fuels accepts the form's names and the list page's suffixes (D, H, E).
// Petrol/gas cannot be told from petrol on the list page, so asking for it
// also accepts plain petrol.
func fuels(f *Field, names []string) ([]string, error) {
	var out []string
	for _, n := range names {
		if canon, ok := fuelSuffix[strings.ToUpper(strings.TrimSpace(n))]; ok && n != "" {
			n = canon
		}
		o, err := f.lookup(n)
		if err != nil {
			return nil, fmt.Errorf("filter \"fuel\": %w", err)
		}
		out = append(out, o.Name)
		if o.Name == "Benzīns/gāze" {
			out = append(out, "Benzīns")
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
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
