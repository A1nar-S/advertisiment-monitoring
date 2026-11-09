package ss

import (
	"log/slog"
	"sync"

	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
)

var (
	categoriesOnce sync.Once
	categories     []sources.Category
)

// Categories implements sources.Catalog: the codebooks as form descriptions,
// for the bot's autocomplete and the dashboard's editor.
func (*Client) Categories() []sources.Category {
	categoriesOnce.Do(func() {
		for _, c := range []struct {
			id, label string
			load      func() (*Codebook, error)
		}{
			{CategoryCars, "Cars", CarsCodebook},
			{CategoryFlats, "Flats", FlatsCodebook},
		} {
			cb, err := c.load()
			if err != nil {
				slog.Error("codebook", "category", c.id, "err", err)
				continue
			}
			categories = append(categories, sources.Category{ID: c.id, Label: c.label, Fields: cb.fieldSpecs()})
		}
	})
	return categories
}

func (cb *Codebook) fieldSpecs() []sources.FieldSpec {
	var out []sources.FieldSpec
	for _, f := range cb.Fields {
		spec := sources.FieldSpec{
			Key: f.Key, Label: f.Label, Kind: f.Kind, Detail: f.From == fromAd,
			// Flats need a region and deal to pick feeds; a car's city is just a filter.
			Required: f.From == fromFeed && (f.Key == "region" || f.Key == "deal"),
			Options:  f.optionNames(),
		}
		switch f.Key {
		case "make":
			spec.Options = cb.makeNames()
		case "model":
			spec.DependsOn, spec.OptionsBy = "make", map[string][]string{}
			for _, m := range cb.Makes {
				if len(m.Models) > 0 {
					spec.OptionsBy[m.Name] = optionNames(m.Models)
				}
			}
		case "region":
			if len(cb.Regions) > 0 {
				spec.Options = cb.regionNames()
			}
		case "district":
			spec.DependsOn, spec.OptionsBy = "region", map[string][]string{}
			for _, r := range cb.Regions {
				if len(r.Districts) > 0 {
					spec.OptionsBy[r.Name] = optionNames(r.Districts)
				}
			}
		}
		if f.Kind == kindRange {
			spec.Options = nil
		}
		out = append(out, spec)
		if f.Key == "model" && cb.Category == CarsCategory {
			out = append(out, sources.FieldSpec{Key: KeywordsKey, Label: "Atslēgvārdi", Kind: "text"})
		}
	}
	return out
}

func optionNames(os []Option) []string {
	out := make([]string, len(os))
	for i, o := range os {
		out[i] = o.Name
	}
	return out
}
