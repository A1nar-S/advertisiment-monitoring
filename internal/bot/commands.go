package bot

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions"
)

const (
	CmdWatch     = "watch"
	CmdEdit      = "edit"
	CmdSearches  = "searches"
	CmdMatches   = "matches"
	CmdUnwatch   = "unwatch"
	CmdDashboard = "dashboard"
	CmdSetup     = "setup"

	SetupStatusChannel = "status-channel"
	SetupStatusOff     = "status-off"
	SetupShow          = "show"
)

// Command describes a command for adapters to register in their platform's
// form (Discord slash commands; Telegram /commands with key=value args).
type Command struct {
	Name, Description string
	Subcommands       []Command
	Params            []Param
	// ManagersOnly hides the command from members who can't manage the
	// server (Discord: Manage Server). The bot still checks for an admin.
	ManagersOnly bool
}

type ParamKind int

const (
	Text ParamKind = iota
	Number
	Channel // a group channel of the platform
)

type Param struct {
	Name, Description string
	Kind              ParamKind
	Required          bool
	// Complete: suggestions come from Bot.Complete. Choices: a fixed list.
	Complete bool
	Choices  []string
	MaxLen   int
}

// rangeKeys maps the prefix of a *_min/*_max param to its filter key.
var rangeKeys = map[string]string{
	"year": "year", "price": "price", "mileage": "mileage_km", "engine": "engine_l",
	"rooms": "rooms", "area": "area_m2", "floor": "floor",
}

// Commands lists the commands, with choices taken from the source's catalog.
func (b *Bot) Commands() []Command {
	name := Param{Name: "name", Description: "Your label for this search, e.g. Cheap Skoda. Doesn't filter. Reusing a name replaces that search", Kind: Text, Required: true, MaxLen: 80}
	own := Param{Name: "name", Description: "One of your searches", Kind: Text, Required: true, Complete: true}
	channel := Param{Name: "channel", Description: "Post alerts in this channel instead of privately", Kind: Channel}
	clear := Param{Name: "clear", Description: "Filters to remove, comma separated (model, price_max); channel = back to private messages", Kind: Text, Complete: true, MaxLen: 300}
	matchesOf := own
	matchesOf.Required, matchesOf.Description = false, "Only this search; empty = all of yours"

	watch := func(category, desc string) Command {
		return Command{Name: category, Description: desc, Params: slices.Concat([]Param{name}, b.filterParams(category), []Param{channel})}
	}
	// edit takes the same filters, all optional: only those given change.
	edit := func(category, desc string) Command {
		filters := b.filterParams(category)
		for i := range filters {
			filters[i].Required = false
		}
		return Command{Name: category, Description: desc, Params: slices.Concat([]Param{own}, filters, []Param{clear, channel})}
	}

	return []Command{
		{Name: CmdWatch, Description: "Get alerts for new ss.com ads; the same name again replaces the search", Subcommands: []Command{
			watch("cars", "Watch ss.com for new car ads"),
			watch("flats", "Watch ss.com for new flat ads"),
		}},
		{Name: CmdEdit, Description: "Change some filters of a search and keep the rest", Subcommands: []Command{
			edit("cars", "Change a car search"),
			edit("flats", "Change a flat search"),
		}},
		{Name: CmdSearches, Description: "List, pause or delete your searches"},
		{Name: CmdMatches, Description: "Show the latest ads your searches alerted you about", Params: []Param{
			matchesOf,
			{Name: "count", Description: fmt.Sprintf("How many, up to %d (default %d)", maxMatches, defaultMatches), Kind: Number},
		}},
		{Name: CmdUnwatch, Description: "Delete one of your searches", Params: []Param{own}},
		{Name: CmdDashboard, Description: "Get a login link to edit your searches in the browser"},
		{Name: CmdSetup, Description: "Bot settings, for the bot's admins", ManagersOnly: true, Subcommands: []Command{
			{Name: SetupStatusChannel, Description: "Post the service's start, stop and error messages in a channel", Params: []Param{
				{Name: "channel", Description: "Where status messages go", Kind: Channel, Required: true},
			}},
			{Name: SetupStatusOff, Description: "Stop posting status messages"},
			{Name: SetupShow, Description: "Show the current settings"},
		}},
	}
}

// filterParams lists a category's filter params, in form order.
func (b *Bot) filterParams(category string) []Param {
	text := func(name, desc string) Param {
		return Param{Name: name, Description: desc, Kind: Text, Complete: true, MaxLen: 300}
	}
	num := func(name, desc string) Param { return Param{Name: name, Description: desc, Kind: Number} }
	choice := func(name, desc, key string) Param {
		return Param{Name: name, Description: desc, Kind: Text, Choices: b.fields[category][key].Options}
	}
	switch category {
	case "cars":
		return []Param{
			text("make", "Makes, comma separated (Skoda, Toyota)"),
			text("model", "Models of those makes (Octavia, Passat*)"),
			{Name: "keywords", Description: "Words in the ad text, any of (quattro, 4x4). May load the ad page: set make/model too", Kind: Text, MaxLen: 300},
			text("region", "Cities or regions, comma separated (Rīga, Rīgas rajons); empty = all Latvia"),
			num("year_min", "Built in or after"), num("year_max", "Built in or before"),
			num("price_min", "Price from, EUR"), num("price_max", "Price up to, EUR"),
			num("mileage_max", "Mileage up to, km"),
			num("engine_min", "Engine from, litres"), num("engine_max", "Engine up to, litres"),
			text("fuel", "Fuel types, comma separated"),
			choice("gearbox", "Gearbox", "gearbox"),
			text("body", "Body types, comma separated"),
			text("color", "Colors, comma separated"),
		}
	case "flats":
		region := text("region", "City or region (Rīga)")
		region.Required = true
		deal := choice("deal", "Izīrē = for rent, Pārdod = for sale", "deal")
		deal.Required = true
		return []Param{
			region, deal,
			text("district", "Districts, comma separated (Centrs, Teika); empty = whole region"),
			num("rooms_min", "Rooms from"), num("rooms_max", "Rooms up to"),
			num("price_min", "Price from, EUR"), num("price_max", "Price up to, EUR"),
			num("area_min", "Area from, m²"), num("floor_min", "Floor from"),
			text("series", "Building series, comma separated (Jaun., 119.)"),
		}
	}
	return nil
}

// FiltersFromArgs turns watch args into form-style filters, the shape the
// dashboard sends too: lists for names, {min, max} for numbers.
func FiltersFromArgs(args map[string]any) map[string]any {
	filters := map[string]any{}
	for name, v := range args {
		if name == "name" || name == "channel" || name == "clear" {
			continue
		}
		if key, bound, ok := rangeParam(name); ok {
			r, _ := filters[key].(map[string]any)
			if r == nil {
				r = map[string]any{}
				filters[key] = r
			}
			switch n := v.(type) {
			case float64:
				r[bound] = n
			case string: // text platforms send numbers as text
				if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
					r[bound] = f
				} else {
					r[bound] = n // let validation explain
				}
			}
			continue
		}
		if s, ok := v.(string); ok {
			if items := SplitList(s); len(items) > 0 {
				filters[name] = toAny(items)
			}
		}
	}
	return filters
}

// rangeParam reads "price_max" as the filter key "price" and bound "max".
func rangeParam(name string) (key, bound string, ok bool) {
	i := strings.LastIndexByte(name, '_')
	if i <= 0 {
		return "", "", false
	}
	key, ok = rangeKeys[name[:i]]
	bound = name[i+1:]
	return key, bound, ok && (bound == "min" || bound == "max")
}

// SplitList reads "Skoda, Toyota," as [Skoda Toyota].
func SplitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
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

// Describe renders filters for a reply: "make Skoda, Toyota · price ≤ 7000".
func Describe(filters map[string]any) string {
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var parts []string
	for _, k := range keys {
		switch v := filters[k].(type) {
		case map[string]any:
			lo, hasLo := v["min"]
			hi, hasHi := v["max"]
			switch {
			case hasLo && hasHi:
				parts = append(parts, fmt.Sprintf("%s %v–%v", k, lo, hi))
			case hasLo:
				parts = append(parts, fmt.Sprintf("%s ≥ %v", k, lo))
			case hasHi:
				parts = append(parts, fmt.Sprintf("%s ≤ %v", k, hi))
			}
		case []any:
			items := make([]string, len(v))
			for i, e := range v {
				items[i] = fmt.Sprint(e)
			}
			parts = append(parts, k+" "+strings.Join(items, ", "))
		}
	}
	if len(parts) == 0 {
		return "everything"
	}
	return strings.Join(parts, " · ")
}

// listing renders the owner's searches with menus to pause or delete them.
func (b *Bot) listing(ctx context.Context, o subscriptions.Owner, note string) (Reply, error) {
	vs, err := b.searches.ListSearches(ctx, o, false)
	if err != nil {
		return Reply{}, err
	}
	if len(vs) == 0 {
		return Reply{Text: note + "You have no searches yet. Start one with **watch cars** or **watch flats**."}, nil
	}
	var sb strings.Builder
	sb.WriteString(note)
	limit := ""
	if b.maxPerUser > 0 {
		limit = fmt.Sprintf("/%d", b.maxPerUser)
	}
	fmt.Fprintf(&sb, "**Your searches** (%d%s):\n", len(vs), limit)
	toggle := Menu{Action: ActionToggle, Placeholder: "Pause or resume…"}
	del := Menu{Action: ActionDelete, Placeholder: "Delete…"}
	for _, v := range vs {
		state, verb := "▶️", "Pause"
		if !v.Enabled {
			state, verb = "⏸️", "Resume"
		}
		where := b.m.Mention(v.Channel.Target)
		if strings.HasPrefix(v.Channel.Label, PrivateLabel) {
			where = "private"
		}
		fmt.Fprintf(&sb, "%s **%s** — %s: %s · %s · %d alerts\n",
			state, v.Name, v.Category, Truncate(Describe(v.Filters), 160), where, v.Matches)
		id := strconv.FormatInt(v.ID, 10)
		toggle.Options = append(toggle.Options, MenuOption{Label: Truncate(verb+" "+v.Name, 100), Value: id})
		del.Options = append(del.Options, MenuOption{Label: Truncate("Delete "+v.Name, 100), Value: id})
	}
	sb.WriteString("Change one with **edit**; see its ads with **matches**.")
	return Reply{Text: sb.String(), Menus: []Menu{toggle, del}}, nil
}
