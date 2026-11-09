package bot

import (
	"context"
	"slices"
	"strings"
)

// MaxSuggestions is how many suggestions platforms show (Discord: 25).
const MaxSuggestions = 25

// Complete suggests values for the param being typed. Comma lists are
// completed on their last item ("Skoda, To" → "Skoda, Toyota"); models are
// narrowed by the makes already given, districts by the region. Search
// names come from the user's own searches.
func (b *Bot) Complete(ctx context.Context, u User, c Call, param string) []string {
	typed := argString(c.Args, param)
	switch {
	case param == "name" && c.Command != CmdWatch:
		return b.completeOwn(ctx, u, c.Sub, typed)
	case param == "clear" && c.Command == CmdEdit:
		return completeList(typed, append(b.clearable(c.Sub), "channel"))
	}
	f, ok := b.fields[c.Sub][param]
	if !ok {
		return nil
	}

	options := f.Options
	if f.DependsOn != "" {
		options = nil
		parents := SplitList(argString(c.Args, f.DependsOn))
		for parent, opts := range f.OptionsBy {
			if len(parents) == 0 || slices.ContainsFunc(parents, func(p string) bool { return fold(p) == fold(parent) }) {
				options = append(options, opts...)
			}
		}
		slices.Sort(options)
		options = slices.Compact(options)
	}
	return completeList(typed, options)
}

// completeList completes the last item of a comma list from options,
// leaving out those already chosen.
func completeList(typed string, options []string) []string {
	// "Skoda, To": keep "Skoda, ", complete "To".
	done, fragment := "", typed
	if i := strings.LastIndexByte(typed, ','); i >= 0 {
		done, fragment = strings.TrimSpace(typed[:i])+", ", strings.TrimSpace(typed[i+1:])
	}
	chosen := SplitList(done)
	var prefix, inside []string
	for _, o := range options {
		if slices.ContainsFunc(chosen, func(c string) bool { return fold(c) == fold(o) }) {
			continue
		}
		switch {
		case strings.HasPrefix(fold(o), fold(fragment)):
			prefix = append(prefix, o)
		case strings.Contains(fold(o), fold(fragment)):
			inside = append(inside, o)
		}
	}
	var out []string
	for _, o := range append(prefix, inside...) {
		if v := done + o; len(v) <= 100 && len(out) < MaxSuggestions {
			out = append(out, v)
		}
	}
	return out
}

// completeOwn suggests the user's search names; of one category if given.
func (b *Bot) completeOwn(ctx context.Context, u User, category, typed string) []string {
	o, err := b.owner(ctx, u)
	if err != nil {
		return nil
	}
	vs, err := b.searches.ListSearches(ctx, o, false)
	if err != nil {
		return nil
	}
	var out []string
	for _, v := range vs {
		if (category == "" || v.Category == category) && strings.Contains(fold(v.Name), fold(typed)) && len(out) < MaxSuggestions {
			out = append(out, v.Name)
		}
	}
	return out
}

// fold compares names ignoring case and Latvian diacritics.
func fold(s string) string { return diacritics.Replace(strings.ToLower(strings.TrimSpace(s))) }

var diacritics = strings.NewReplacer(
	"ā", "a", "č", "c", "ē", "e", "ģ", "g", "ī", "i", "ķ", "k", "ļ", "l", "ņ", "n",
	"š", "s", "ū", "u", "ž", "z", "ō", "o", "ŗ", "r",
)
