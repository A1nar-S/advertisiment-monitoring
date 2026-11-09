// Package bot is the chat bot people use to manage their own searches:
// watch, edit, searches, matches, unwatch, dashboard; setup for admins. It knows no chat platform: a platform
// adapter (discordbot; Telegram later) turns its Commands into the
// platform's commands, calls Run/Complete/Act and renders the Reply. All
// searches go through the subscriptions slice, with the same validation as
// the dashboard.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/sources"
	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions"
)

// Messenger is what the bot needs from a chat platform.
type Messenger interface {
	// Platform names it ("discord"); it is also the alert channel kind the
	// notifier delivers through.
	Platform() string
	// DirectChat opens (or finds) the private chat with a user: where alerts
	// go by default.
	DirectChat(ctx context.Context, userID string) (string, error)
	Send(ctx context.Context, target, text string) error
	// Mention renders a group channel in a reply ("<#123>" on Discord).
	Mention(target string) string
	// ChannelName names a group channel for the dashboard ("#cars"); best
	// effort, "" when unknown.
	ChannelName(ctx context.Context, target string) string
}

// Searches is what the bot needs from the subscriptions slice.
type Searches interface {
	PlatformOwner(ctx context.Context, platform, externalID, displayName string) (subscriptions.Owner, error)
	EnsureChannel(ctx context.Context, o subscriptions.Owner, kind, target, label string) (subscriptions.ChannelView, error)
	SaveSearchByName(ctx context.Context, o subscriptions.Owner, in subscriptions.SearchInput) (subscriptions.SearchView, bool, error)
	UpdateSearch(ctx context.Context, o subscriptions.Owner, id int64, in subscriptions.SearchInput) (subscriptions.SearchView, error)
	FindSearch(ctx context.Context, o subscriptions.Owner, name string) (subscriptions.SearchView, error)
	GetSearch(ctx context.Context, o subscriptions.Owner, id int64) (subscriptions.SearchView, error)
	ListSearches(ctx context.Context, o subscriptions.Owner, all bool) ([]subscriptions.SearchView, error)
	SetEnabled(ctx context.Context, o subscriptions.Owner, id int64, enabled bool) error
	DeleteSearch(ctx context.Context, o subscriptions.Owner, id int64) error
	NewLoginToken(ctx context.Context, o subscriptions.Owner) (string, error)
	RecentMatches(ctx context.Context, o subscriptions.Owner, searchID int64, limit int) ([]subscriptions.MatchRef, error)
	StatusChannel(ctx context.Context) (events.Channel, bool, error)
	SetStatusChannel(ctx context.Context, o subscriptions.Owner, ch events.Channel) error
	Categories() map[string][]sources.Category
}

// Ad is what a match shows of a stored ad.
type Ad struct{ Title, URL, Price, Location string }

// Ads looks up stored ads by external ID, to show matches; purged ones are
// missing from the result.
type Ads interface {
	Ads(ctx context.Context, source string, ids []string) (map[string]Ad, error)
}

// User is who is talking, as the platform knows them.
type User struct{ ID, Name string }

// Call is one command invocation: watch cars name=… make=… price_max=7000.
// Args hold strings or float64 numbers; "channel" is a group channel target.
type Call struct {
	Command, Sub string
	Args         map[string]any
	// Postable says, per group channel target in Args, whether the user may
	// post there themselves (or manage it). The adapter fills it from the
	// platform's permissions; the bot posts only where the user could.
	Postable map[string]bool
}

// Reply is the bot's answer; the adapter renders Text (with **bold**) and
// turns each Menu into its own widget (a select menu, inline buttons).
type Reply struct {
	Text  string
	Menus []Menu
}

// Menu offers one action on one of several items; choosing calls Act.
type Menu struct {
	Action      string // ActionToggle, ActionDelete
	Placeholder string
	Options     []MenuOption
}

type MenuOption struct{ Label, Value string }

const (
	ActionToggle = "toggle"
	ActionDelete = "delete"
)

// Bot answers calls.
type Bot struct {
	m          Messenger
	searches   Searches
	ads        Ads
	now        func() time.Time
	publicURL  string
	maxPerUser int
	// fields of each category by key, from the source's catalog.
	fields map[string]map[string]sources.FieldSpec
}

func New(m Messenger, s Searches, ads Ads, publicURL string, maxPerUser int) *Bot {
	b := &Bot{m: m, searches: s, ads: ads, now: time.Now, publicURL: strings.TrimRight(publicURL, "/"), maxPerUser: maxPerUser,
		fields: map[string]map[string]sources.FieldSpec{}}
	for _, c := range s.Categories()[subscriptions.DefaultSource] {
		b.fields[c.ID] = map[string]sources.FieldSpec{}
		for _, f := range c.Fields {
			b.fields[c.ID][f.Key] = f
		}
	}
	return b
}

// Run answers a command. Mistakes in what the user wrote come back as the
// reply; other errors are logged and reported vaguely.
func (b *Bot) Run(ctx context.Context, u User, c Call) Reply {
	r, err := b.run(ctx, u, c)
	if err != nil {
		return Reply{Text: b.errorText(c.Command, err)}
	}
	return r
}

func (b *Bot) run(ctx context.Context, u User, c Call) (Reply, error) {
	o, err := b.owner(ctx, u)
	if err != nil {
		return Reply{}, err
	}
	switch c.Command {
	case CmdWatch:
		text, err := b.watch(ctx, o, c)
		return Reply{Text: text}, err
	case CmdEdit:
		text, err := b.edit(ctx, o, c)
		return Reply{Text: text}, err
	case CmdSearches:
		return b.listing(ctx, o, "")
	case CmdMatches:
		text, err := b.matches(ctx, o, c)
		return Reply{Text: text}, err
	case CmdSetup:
		text, err := b.setup(ctx, o, c)
		return Reply{Text: text}, err
	case CmdUnwatch:
		v, err := b.searches.FindSearch(ctx, o, argString(c.Args, "name"))
		if err != nil {
			return Reply{}, err
		}
		if err := b.searches.DeleteSearch(ctx, o, v.ID); err != nil {
			return Reply{}, err
		}
		return Reply{Text: fmt.Sprintf("🗑️ Deleted **%s**.", v.Name)}, nil
	case CmdDashboard:
		// The private chat is where searches made in the browser alert.
		if _, err := b.directChannel(ctx, o); err != nil {
			return Reply{}, err
		}
		token, err := b.searches.NewLoginToken(ctx, o)
		if err != nil {
			return Reply{}, err
		}
		return Reply{Text: fmt.Sprintf("🔐 Your dashboard login link (single use, valid 10 minutes, don't share it):\n%s/api/login?token=%s",
			b.publicURL, token)}, nil
	}
	return Reply{}, fmt.Errorf("unknown command %q", c.Command)
}

// Act runs a menu choice (pause/resume, delete) and returns the refreshed list.
func (b *Bot) Act(ctx context.Context, u User, action, value string) Reply {
	r, err := b.act(ctx, u, action, value)
	if err != nil {
		return Reply{Text: b.errorText(action, err)}
	}
	return r
}

func (b *Bot) act(ctx context.Context, u User, action, value string) (Reply, error) {
	o, err := b.owner(ctx, u)
	if err != nil {
		return Reply{}, err
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return b.listing(ctx, o, "")
	}
	v, err := b.searches.GetSearch(ctx, o, id)
	if err != nil {
		return b.listing(ctx, o, "❌ That search is gone.\n")
	}
	var note string
	switch action {
	case ActionToggle:
		err = b.searches.SetEnabled(ctx, o, id, !v.Enabled)
		note = fmt.Sprintf("⏸️ Paused **%s**.\n", v.Name)
		if !v.Enabled {
			note = fmt.Sprintf("▶️ Resumed **%s**.\n", v.Name)
		}
	case ActionDelete:
		err = b.searches.DeleteSearch(ctx, o, id)
		note = fmt.Sprintf("🗑️ Deleted **%s**.\n", v.Name)
	default:
		return Reply{}, fmt.Errorf("unknown action %q", action)
	}
	if err != nil {
		return Reply{}, err
	}
	return b.listing(ctx, o, note)
}

// watch saves a search from "watch <category>".
func (b *Bot) watch(ctx context.Context, o subscriptions.Owner, c Call) (string, error) {
	target := argString(c.Args, "channel")
	if err := b.mayPost(c, target); err != nil {
		return "", err
	}
	ch, direct, err := b.alertChannel(ctx, o, target)
	if err != nil {
		return "", err
	}
	v, created, err := b.searches.SaveSearchByName(ctx, o, subscriptions.SearchInput{
		Name: argString(c.Args, "name"), Category: c.Sub, Filters: FiltersFromArgs(c.Args),
		ChannelID: ch.ID, Origin: b.m.Platform(),
	})
	if err != nil {
		return "", err
	}
	verb := "Updated"
	if created {
		verb = "Saved"
	}
	reply := fmt.Sprintf("✅ %s **%s** (%s): %s\nAlerts go to %s, for ads posted from now on.",
		verb, v.Name, v.Category, Describe(v.Filters), b.where(ch.Target, direct))
	return reply + b.greet(ctx, o, v.Name, ch.Target, direct), nil
}

// edit changes the filters given in "edit <category>", removes those named
// in clear, and keeps the rest of the search.
func (b *Bot) edit(ctx context.Context, o subscriptions.Owner, c Call) (string, error) {
	v, err := b.searches.FindSearch(ctx, o, argString(c.Args, "name"))
	if err != nil {
		return "", err
	}
	if v.Category != c.Sub {
		return "", &sources.InvalidSearch{Msg: fmt.Sprintf("**%s** is a %s search; use **edit %s**.", v.Name, v.Category, v.Category)}
	}

	filters := maps.Clone(v.Filters)
	toPrivate := false
	clearable := b.clearable(c.Sub)
	for _, p := range SplitList(argString(c.Args, "clear")) {
		p = strings.ToLower(p)
		switch {
		case p == "channel":
			toPrivate = true
		case slices.Contains(clearable, p):
			clearFilter(filters, p)
		default:
			return "", &sources.InvalidSearch{Msg: fmt.Sprintf("can't clear %q; use option names: %s, channel", p, strings.Join(clearable, ", "))}
		}
	}
	given := FiltersFromArgs(c.Args)
	for k, nv := range given {
		if bounds, ok := nv.(map[string]any); ok {
			old, _ := filters[k].(map[string]any)
			merged := maps.Clone(old)
			if merged == nil {
				merged = map[string]any{}
			}
			maps.Copy(merged, bounds)
			nv = merged
		}
		filters[k] = nv
	}

	target := argString(c.Args, "channel")
	if len(given) == 0 && target == "" && argString(c.Args, "clear") == "" {
		return "", &sources.InvalidSearch{Msg: "nothing to change: give the filters to change, or clear: to remove some."}
	}
	if err := b.mayPost(c, target); err != nil {
		return "", err
	}
	in := subscriptions.SearchInput{Name: v.Name, Source: v.Source, Category: v.Category, Filters: filters, Origin: b.m.Platform()}
	// An explicit channel wins over clear: channel.
	moved, direct := target != "" || toPrivate, false
	if moved {
		ch, d, err := b.alertChannel(ctx, o, target)
		if err != nil {
			return "", err
		}
		in.ChannelID, target, direct = ch.ID, ch.Target, d
	}
	if v, err = b.searches.UpdateSearch(ctx, o, v.ID, in); err != nil {
		return "", err
	}

	reply := fmt.Sprintf("✏️ Updated **%s** (%s): %s", v.Name, v.Category, Describe(v.Filters))
	if !moved {
		return reply, nil
	}
	reply += "\nAlerts now go to " + b.where(target, direct) + "."
	return reply + b.greet(ctx, o, v.Name, target, direct), nil
}

// clearable lists the names "clear" accepts for a category: its filter
// params, plus each range as a whole ("price" for price_min and price_max).
func (b *Bot) clearable(category string) []string {
	var out []string
	for _, p := range b.filterParams(category) {
		out = append(out, p.Name)
		if _, _, ok := rangeParam(p.Name); ok {
			if whole := p.Name[:strings.LastIndexByte(p.Name, '_')]; !slices.Contains(out, whole) {
				out = append(out, whole)
			}
		}
	}
	return out
}

// clearFilter removes a filter param ("model", "price_max") or a whole range
// ("price") from form-style filters.
func clearFilter(filters map[string]any, param string) {
	if key, bound, ok := rangeParam(param); ok {
		if r, _ := filters[key].(map[string]any); r != nil {
			r = maps.Clone(r)
			delete(r, bound)
			filters[key] = r
			if len(r) == 0 {
				delete(filters, key)
			}
		}
		return
	}
	if key, ok := rangeKeys[param]; ok {
		delete(filters, key)
		return
	}
	delete(filters, param)
}

const (
	defaultMatches = 10
	maxMatches     = 25
)

// matches lists the latest ads the owner was alerted about, from one search
// or all of them.
func (b *Bot) matches(ctx context.Context, o subscriptions.Owner, c Call) (string, error) {
	n := defaultMatches
	if f, ok := argNumber(c.Args, "count"); ok && f >= 1 {
		n = min(int(f), maxMatches)
	}
	var searchID int64
	name := argString(c.Args, "name")
	if name != "" {
		v, err := b.searches.FindSearch(ctx, o, name)
		if err != nil {
			return "", err
		}
		searchID, name = v.ID, v.Name
	}
	refs, err := b.searches.RecentMatches(ctx, o, searchID, n)
	if err != nil {
		return "", err
	}
	if len(refs) == 0 {
		if name != "" {
			return fmt.Sprintf("No alerts for **%s** yet. Only ads posted after the search was saved count.", name), nil
		}
		return "No alerts yet. Only ads posted after a search was saved count.", nil
	}

	ids := map[string][]string{}
	for _, m := range refs {
		ids[m.Source] = append(ids[m.Source], m.ExternalID)
	}
	found := map[string]map[string]Ad{}
	for source, list := range ids {
		if found[source], err = b.ads.Ads(ctx, source, list); err != nil {
			return "", err
		}
	}

	var sb strings.Builder
	if name != "" {
		fmt.Fprintf(&sb, "**Latest alerts for %s:**\n", name)
	} else {
		sb.WriteString("**Your latest alerts:**\n")
	}
	now := b.now()
	for i, m := range refs {
		var line string
		if l, ok := found[m.Source][m.ExternalID]; ok {
			line = fmt.Sprintf("• [%s](<%s>)", escape(Truncate(l.Title, 90)), l.URL)
			for _, part := range []string{l.Price, l.Location} {
				if part != "" {
					line += " · " + escape(part)
				}
			}
		} else {
			line = fmt.Sprintf("• ad %s (no longer stored)", m.ExternalID)
		}
		line += " · " + ago(now.Sub(m.At))
		if name == "" {
			line += " — " + escape(m.SearchName)
		}
		// Discord allows 2000 characters; leave room for the note.
		if len([]rune(sb.String()))+len([]rune(line)) > 1900 {
			fmt.Fprintf(&sb, "…and %d more; ask for fewer with count.", len(refs)-i)
			break
		}
		sb.WriteString(line + "\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ago renders a duration as "5m ago", "3h ago", "2d ago".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// escape keeps ad text from turning into markdown.
var escape = strings.NewReplacer(
	`\`, `\\`, "*", `\*`, "_", `\_`, "~", `\~`, "`", "\\`", "|", `\|`, "[", `\[`, "]", `\]`, "<", `\<`,
).Replace

// mayPost refuses a group channel the user can't post in themselves, so
// nobody can make the bot post where they may not. Empty = private chat.
func (b *Bot) mayPost(c Call, target string) error {
	if target == "" || c.Postable[target] {
		return nil
	}
	return &sources.InvalidSearch{Msg: fmt.Sprintf("you can't post in %s, so I won't post there for you. Pick a channel you can write in, or leave channel out to get alerts by DM.", b.m.Mention(target))}
}

// setup changes service-wide settings; admins only.
func (b *Bot) setup(ctx context.Context, o subscriptions.Owner, c Call) (string, error) {
	if !o.Admin {
		return "", subscriptions.ErrNotAdmin
	}
	switch c.Sub {
	case SetupStatusChannel:
		target := argString(c.Args, "channel")
		if target == "" {
			return "", &sources.InvalidSearch{Msg: "choose a channel."}
		}
		if err := b.mayPost(c, target); err != nil {
			return "", err
		}
		where := b.m.Mention(target)
		if err := b.m.Send(ctx, target, "✅ The ad monitor's status messages (started, stopped, errors) will be posted here."); err != nil {
			slog.Warn("cannot reach status channel", "platform", b.m.Platform(), "target", target, "err", err)
			return "", &sources.InvalidSearch{Msg: fmt.Sprintf("I can't post in %s. Let me send messages there, then try again.", where)}
		}
		if err := b.searches.SetStatusChannel(ctx, o, events.Channel{Kind: b.m.Platform(), Target: target}); err != nil {
			return "", err
		}
		return fmt.Sprintf("✅ Status messages now go to %s.", where), nil
	case SetupStatusOff:
		if err := b.searches.SetStatusChannel(ctx, o, events.Channel{Kind: b.m.Platform()}); err != nil {
			return "", err
		}
		return "🔕 Status messages are off. Turn them back on with **setup status-channel**.", nil
	case SetupShow:
		ch, ok, err := b.searches.StatusChannel(ctx)
		if err != nil {
			return "", err
		}
		status := b.m.Mention(ch.Target)
		switch {
		case !ok:
			status = "not set here, so ADMIN_CHANNEL_ID from .env applies (off if that is empty)"
		case ch.Target == "":
			status = "off"
		case ch.Kind != b.m.Platform():
			status = ch.Kind + " channel " + ch.Target
		}
		return fmt.Sprintf("**Settings**\nStatus messages: %s\nYou are one of the bot's admins.", status), nil
	}
	return "", fmt.Errorf("unknown setup %q", c.Sub)
}

// alertChannel registers where alerts go: the group channel target, or the
// private chat when target is empty (direct).
func (b *Bot) alertChannel(ctx context.Context, o subscriptions.Owner, target string) (ch subscriptions.ChannelView, direct bool, err error) {
	if target == "" {
		ch, err = b.directChannel(ctx, o)
		return ch, true, err
	}
	ch, err = b.searches.EnsureChannel(ctx, o, b.m.Platform(), target, b.m.ChannelName(ctx, target))
	return ch, false, err
}

func (b *Bot) where(target string, direct bool) string {
	if direct {
		return "your private messages"
	}
	return b.m.Mention(target)
}

// greet posts a hello where a search's alerts will go, and returns a warning
// for the reply if it can't.
func (b *Bot) greet(ctx context.Context, o subscriptions.Owner, search, target string, direct bool) string {
	hello := fmt.Sprintf("🔔 Alerts for **%s** will arrive here.", search)
	if !direct {
		hello = fmt.Sprintf("🔔 %s's search **%s** will post its alerts here.", o.Name, search)
	}
	if err := b.m.Send(ctx, target, hello); err != nil {
		slog.Warn("cannot reach alert target", "platform", b.m.Platform(), "target", target, "err", err)
		if direct {
			return "\n⚠️ I can't message you privately. Allow messages from me (on Discord: server menu → Privacy Settings → Direct Messages), or choose a channel."
		}
		return "\n⚠️ I can't post in that channel. Let me send messages there, or leave the channel out for private messages."
	}
	return ""
}

// PrivateLabel is how a private-chat alert channel is shown.
const PrivateLabel = "Private messages"

// directChannel registers the user's private chat as an alert channel.
func (b *Bot) directChannel(ctx context.Context, o subscriptions.Owner) (subscriptions.ChannelView, error) {
	t, err := b.m.DirectChat(ctx, o.ExternalID)
	if err != nil {
		return subscriptions.ChannelView{}, fmt.Errorf("open direct chat: %w", err)
	}
	return b.searches.EnsureChannel(ctx, o, b.m.Platform(), t, PrivateLabel+" ("+b.m.Platform()+")")
}

func (b *Bot) owner(ctx context.Context, u User) (subscriptions.Owner, error) {
	if u.ID == "" {
		return subscriptions.Owner{}, errors.New("call without a user")
	}
	return b.searches.PlatformOwner(ctx, b.m.Platform(), u.ID, u.Name)
}

func (b *Bot) errorText(what string, err error) string {
	var inv *sources.InvalidSearch
	switch {
	case errors.As(err, &inv):
		return "❌ " + inv.Msg
	case errors.Is(err, subscriptions.ErrNotFound):
		return "❌ No such search."
	case errors.Is(err, subscriptions.ErrNotAdmin):
		return "❌ Only the bot's admins can change its settings: the owner of the Discord app, or the accounts in ADMIN_USERS."
	}
	slog.Error("bot", "platform", b.m.Platform(), "call", what, "err", err)
	return "❌ Something went wrong; try again in a moment."
}

// argNumber reads a number arg; text platforms send it as text.
func argNumber(args map[string]any, name string) (float64, bool) {
	switch v := args[name].(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	}
	return 0, false
}

func argString(args map[string]any, name string) string {
	switch v := args[name].(type) {
	case string:
		return strings.TrimSpace(v)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// Truncate shortens s to n runes, for platforms' message limits.
func Truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
