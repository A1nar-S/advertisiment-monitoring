// Package discordbot puts the platform-neutral bot on Discord: slash
// commands with autocomplete, ephemeral replies, select menus. It is also
// the bot's bot.Messenger on Discord.
package discordbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/A1nar-S/advertisiment-monitoring/internal/bot"
)

// Platform is the name the bot and the notifier know Discord by.
const Platform = "discord"

// Adapter connects a bot.Bot to Discord.
type Adapter struct {
	s    *discordgo.Session
	core *bot.Bot
}

func New(token string) (*Adapter, error) {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("discord session: %w", err)
	}
	s.Identify.Intents = discordgo.IntentsGuilds // interactions need no other intent
	return &Adapter{s: s}, nil
}

// --- bot.Messenger ----------------------------------------------------------

func (a *Adapter) Platform() string { return Platform }

func (a *Adapter) DirectChat(ctx context.Context, userID string) (string, error) {
	ch, err := a.s.UserChannelCreate(userID, discordgo.WithContext(ctx))
	if err != nil {
		return "", err
	}
	return ch.ID, nil
}

func (a *Adapter) Send(ctx context.Context, target, text string) error {
	_, err := a.s.ChannelMessageSend(target, text, discordgo.WithContext(ctx))
	return err
}

func (a *Adapter) Mention(target string) string { return "<#" + target + ">" }

func (a *Adapter) ChannelName(ctx context.Context, target string) string {
	ch, err := a.s.Channel(target, discordgo.WithContext(ctx))
	if err != nil || ch.Name == "" {
		return ""
	}
	return "#" + ch.Name
}

// --- serving ------------------------------------------------------------------

// Serve registers the bot's commands (on guildID only when set, which
// updates instantly; globally otherwise) and answers interactions until ctx
// is done.
func (a *Adapter) Serve(ctx context.Context, core *bot.Bot, guildID string) error {
	a.core = core
	ready := make(chan string, 1)
	a.s.AddHandlerOnce(func(_ *discordgo.Session, r *discordgo.Ready) { ready <- r.User.ID })
	a.s.AddHandler(func(_ *discordgo.Session, ic *discordgo.InteractionCreate) { a.handle(ctx, ic.Interaction) })
	if err := a.s.Open(); err != nil {
		return fmt.Errorf("discord gateway: %w", err)
	}
	defer func() { _ = a.s.Close() }()

	var appID string
	select {
	case appID = <-ready:
	case <-time.After(30 * time.Second):
		return errors.New("discord gateway: no Ready event")
	case <-ctx.Done():
		return nil
	}
	if _, err := a.s.ApplicationCommandBulkOverwrite(appID, guildID, ToDiscord(core.Commands()), discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("register commands: %w", err)
	}
	slog.Info("discord bot ready", "app", appID, "guild", guildID)
	<-ctx.Done()
	return nil
}

func (a *Adapter) handle(ctx context.Context, i *discordgo.Interaction) {
	var err error
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		err = a.command(ctx, i)
	case discordgo.InteractionApplicationCommandAutocomplete:
		err = a.autocomplete(ctx, i)
	case discordgo.InteractionMessageComponent:
		err = a.component(ctx, i)
	}
	if err != nil {
		slog.Error("discord interaction", "type", i.Type, "err", err)
	}
}

// command acknowledges at once (Discord allows 3 seconds) and answers by
// editing that private reply.
func (a *Adapter) command(ctx context.Context, i *discordgo.Interaction) error {
	err := a.s.InteractionRespond(i, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
	if err != nil {
		return err
	}
	call, _ := CallOf(i.ApplicationCommandData())
	if target, ok := call.Args["channel"].(string); ok && target != "" {
		call.Postable = map[string]bool{target: a.canPost(i, target)}
	}
	reply := a.core.Run(ctx, userOf(i), call)
	content, components := render(reply)
	_, err = a.s.InteractionResponseEdit(i, &discordgo.WebhookEdit{Content: &content, Components: &components})
	return err
}

func (a *Adapter) autocomplete(ctx context.Context, i *discordgo.Interaction) error {
	call, focused := CallOf(i.ApplicationCommandData())
	var choices []*discordgo.ApplicationCommandOptionChoice
	for _, s := range a.core.Complete(ctx, userOf(i), call, focused) {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: s, Value: s})
	}
	return a.s.InteractionRespond(i, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	})
}

func (a *Adapter) component(ctx context.Context, i *discordgo.Interaction) error {
	data := i.MessageComponentData()
	value := ""
	if len(data.Values) > 0 {
		value = data.Values[0]
	}
	content, components := render(a.core.Act(ctx, userOf(i), data.CustomID, value))
	return a.s.InteractionRespond(i, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Content: content, Components: components},
	})
}

// canPost reports whether the user who sent i may post in the channel, or
// manage it. Discord sends the user's permissions in the channel the command
// came from; for another channel they are worked out from the server's roles
// and the channel's overrides (cached from the gateway, else fetched).
func (a *Adapter) canPost(i *discordgo.Interaction, channelID string) bool {
	if i.Member == nil || i.Member.User == nil {
		return false
	}
	if channelID == i.ChannelID {
		return MayPost(i.Member.Permissions)
	}
	// MessagePermissions computes a member's permissions in a channel from
	// the member's roles; the "message" just carries who and where.
	probe := &discordgo.Message{ChannelID: channelID, Author: i.Member.User, Member: i.Member}
	perms, err := a.s.State.MessagePermissions(probe)
	if err != nil {
		perms, err = a.fetchPermissions(probe, i.GuildID)
	}
	if err != nil {
		slog.Warn("discord permissions", "channel", channelID, "err", err)
		return false
	}
	return MayPost(perms)
}

// fetchPermissions loads the server and channel into the cache, then
// computes the permissions again.
func (a *Adapter) fetchPermissions(probe *discordgo.Message, guildID string) (int64, error) {
	g, err := a.s.Guild(guildID)
	if err != nil {
		return 0, err
	}
	if err := a.s.State.GuildAdd(g); err != nil {
		return 0, err
	}
	ch, err := a.s.Channel(probe.ChannelID)
	if err != nil {
		return 0, err
	}
	if ch.GuildID != guildID {
		return 0, fmt.Errorf("channel %s is not on this server", ch.ID)
	}
	if err := a.s.State.ChannelAdd(ch); err != nil {
		return 0, err
	}
	return a.s.State.MessagePermissions(probe)
}

// MayPost: can view and send messages in the channel, or manage it.
func MayPost(perms int64) bool {
	const viewAndSend = discordgo.PermissionViewChannel | discordgo.PermissionSendMessages
	return perms&discordgo.PermissionAdministrator != 0 ||
		perms&discordgo.PermissionManageChannels != 0 ||
		perms&viewAndSend == viewAndSend
}

// Owners lists the accounts that own the bot's Discord app (its owner, or
// its team's members). The bot treats them as admins.
func (a *Adapter) Owners() ([]string, error) {
	app, err := a.s.Application("@me")
	if err != nil {
		return nil, fmt.Errorf("discord application: %w", err)
	}
	var ids []string
	if app.Team != nil {
		for _, m := range app.Team.Members {
			if m.User != nil && m.MembershipState == discordgo.MembershipStateAccepted {
				ids = append(ids, m.User.ID)
			}
		}
		if app.Team.OwnerID != "" && !slices.Contains(ids, app.Team.OwnerID) {
			ids = append(ids, app.Team.OwnerID)
		}
	} else if app.Owner != nil {
		ids = append(ids, app.Owner.ID)
	}
	return ids, nil
}

func userOf(i *discordgo.Interaction) bot.User {
	u := i.User
	if i.Member != nil && i.Member.User != nil {
		u = i.Member.User
	}
	if u == nil {
		return bot.User{}
	}
	name := u.GlobalName
	if name == "" {
		name = u.Username
	}
	return bot.User{ID: u.ID, Name: name}
}

// --- translation ----------------------------------------------------------------

// ToDiscord turns the bot's commands into guild-only slash commands.
func ToDiscord(cmds []bot.Command) []*discordgo.ApplicationCommand {
	guildOnly := &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild}
	out := make([]*discordgo.ApplicationCommand, 0, len(cmds))
	for _, c := range cmds {
		ac := &discordgo.ApplicationCommand{Name: c.Name, Description: bot.Truncate(c.Description, 100), Contexts: guildOnly}
		if c.ManagersOnly {
			perm := int64(discordgo.PermissionManageGuild)
			ac.DefaultMemberPermissions = &perm
		}
		for _, sub := range c.Subcommands {
			ac.Options = append(ac.Options, &discordgo.ApplicationCommandOption{
				Type: discordgo.ApplicationCommandOptionSubCommand, Name: sub.Name,
				Description: bot.Truncate(sub.Description, 100), Options: options(sub.Params),
			})
		}
		if len(c.Subcommands) == 0 {
			ac.Options = options(c.Params)
		}
		out = append(out, ac)
	}
	return out
}

// options converts params; Discord wants required options first.
func options(ps []bot.Param) []*discordgo.ApplicationCommandOption {
	ps = slices.Clone(ps)
	slices.SortStableFunc(ps, func(a, b bot.Param) int { return boolInt(b.Required) - boolInt(a.Required) })
	out := make([]*discordgo.ApplicationCommandOption, 0, len(ps))
	for _, p := range ps {
		o := &discordgo.ApplicationCommandOption{Name: p.Name, Description: bot.Truncate(p.Description, 100), Required: p.Required}
		switch p.Kind {
		case bot.Number:
			o.Type, o.MinValue = discordgo.ApplicationCommandOptionNumber, new(float64)
		case bot.Channel:
			o.Type, o.ChannelTypes = discordgo.ApplicationCommandOptionChannel, []discordgo.ChannelType{discordgo.ChannelTypeGuildText}
		default:
			o.Type, o.MaxLength = discordgo.ApplicationCommandOptionString, p.MaxLen
			if len(p.Choices) > 0 && len(p.Choices) <= 25 {
				for _, c := range p.Choices {
					o.Choices = append(o.Choices, &discordgo.ApplicationCommandOptionChoice{Name: c, Value: c})
				}
			} else {
				o.Autocomplete = p.Complete || len(p.Choices) > 25
			}
		}
		out = append(out, o)
	}
	return out
}

// CallOf reads a slash command (and, while autocompleting, which option is
// being typed).
func CallOf(data discordgo.ApplicationCommandInteractionData) (call bot.Call, focused string) {
	call = bot.Call{Command: data.Name, Args: map[string]any{}}
	opts := data.Options
	if len(opts) == 1 && opts[0].Type == discordgo.ApplicationCommandOptionSubCommand {
		call.Sub, opts = opts[0].Name, opts[0].Options
	}
	for _, o := range opts {
		call.Args[o.Name] = o.Value // string, float64, or a channel ID string
		if o.Focused {
			focused = o.Name
		}
	}
	return call, focused
}

// render turns a reply into message content and select menus.
func render(r bot.Reply) (string, []discordgo.MessageComponent) {
	components := []discordgo.MessageComponent{}
	for _, m := range r.Menus {
		if len(m.Options) == 0 || len(components) == 5 {
			continue
		}
		menu := discordgo.SelectMenu{CustomID: m.Action, Placeholder: m.Placeholder}
		for _, o := range m.Options {
			if len(menu.Options) < 25 {
				menu.Options = append(menu.Options, discordgo.SelectMenuOption{Label: o.Label, Value: o.Value})
			}
		}
		components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{menu}})
	}
	return bot.Truncate(r.Text, 2000), components
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
