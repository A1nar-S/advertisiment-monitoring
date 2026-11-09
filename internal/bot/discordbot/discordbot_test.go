package discordbot

import (
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/A1nar-S/advertisiment-monitoring/internal/bot"
)

func TestToDiscord(t *testing.T) {
	cmds := ToDiscord([]bot.Command{{Name: "watch", Description: "d", Subcommands: []bot.Command{{Name: "cars", Params: []bot.Param{
		{Name: "make", Kind: bot.Text, Complete: true},
		{Name: "name", Kind: bot.Text, Required: true},
		{Name: "gearbox", Kind: bot.Text, Choices: []string{"Automāts", "Manuāla"}},
		{Name: "price_max", Kind: bot.Number},
		{Name: "channel", Kind: bot.Channel},
	}}}}})
	if len(cmds) != 1 || cmds[0].Contexts == nil || (*cmds[0].Contexts)[0] != discordgo.InteractionContextGuild {
		t.Fatalf("commands = %+v", cmds)
	}
	sub := cmds[0].Options[0]
	if sub.Type != discordgo.ApplicationCommandOptionSubCommand {
		t.Fatalf("sub = %+v", sub)
	}
	opts := sub.Options
	if opts[0].Name != "name" || !opts[0].Required {
		t.Errorf("required options must come first: %s", opts[0].Name)
	}
	byName := map[string]*discordgo.ApplicationCommandOption{}
	for _, o := range opts {
		byName[o.Name] = o
	}
	if !byName["make"].Autocomplete || len(byName["gearbox"].Choices) != 2 || byName["gearbox"].Autocomplete {
		t.Errorf("make/gearbox = %+v / %+v", byName["make"], byName["gearbox"])
	}
	if byName["price_max"].Type != discordgo.ApplicationCommandOptionNumber || byName["channel"].Type != discordgo.ApplicationCommandOptionChannel {
		t.Errorf("types: %v %v", byName["price_max"].Type, byName["channel"].Type)
	}
}

func TestCallOf(t *testing.T) {
	data := discordgo.ApplicationCommandInteractionData{Name: "watch", Options: []*discordgo.ApplicationCommandInteractionDataOption{{
		Name: "cars", Type: discordgo.ApplicationCommandOptionSubCommand,
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "name", Value: "Parents"},
			{Name: "price_max", Value: 7000.0},
			{Name: "make", Value: "Sko", Focused: true},
		},
	}}}
	call, focused := CallOf(data)
	if call.Command != "watch" || call.Sub != "cars" || call.Args["price_max"] != 7000.0 || focused != "make" {
		t.Errorf("call = %+v, focused %q", call, focused)
	}
}

func TestRender(t *testing.T) {
	content, comps := render(bot.Reply{Text: "hi", Menus: []bot.Menu{
		{Action: bot.ActionToggle, Placeholder: "p", Options: []bot.MenuOption{{Label: "Pause A", Value: "1"}}},
		{Action: bot.ActionDelete}, // empty: skipped
	}})
	if content != "hi" || len(comps) != 1 {
		t.Fatalf("render = %q %+v", content, comps)
	}
	menu := comps[0].(discordgo.ActionsRow).Components[0].(discordgo.SelectMenu)
	if menu.CustomID != bot.ActionToggle || menu.Options[0].Value != "1" {
		t.Errorf("menu = %+v", menu)
	}
}

func TestMayPost(t *testing.T) {
	for _, tc := range []struct {
		perms int64
		want  bool
	}{
		{0, false},
		{discordgo.PermissionViewChannel, false},
		{discordgo.PermissionSendMessages, false},
		{discordgo.PermissionViewChannel | discordgo.PermissionSendMessages, true},
		{discordgo.PermissionManageChannels, true},
		{discordgo.PermissionAdministrator, true},
	} {
		if got := MayPost(tc.perms); got != tc.want {
			t.Errorf("MayPost(%b) = %v, want %v", tc.perms, got, tc.want)
		}
	}
}

func TestSetupIsForManagers(t *testing.T) {
	cmds := ToDiscord([]bot.Command{{Name: "setup", ManagersOnly: true}, {Name: "watch"}})
	if p := cmds[0].DefaultMemberPermissions; p == nil || *p != discordgo.PermissionManageGuild {
		t.Errorf("setup permissions = %v", p)
	}
	if cmds[1].DefaultMemberPermissions != nil {
		t.Error("watch should be for everyone")
	}
}
