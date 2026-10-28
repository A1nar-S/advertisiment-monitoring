// Package discord delivers notifications to a Discord channel over the REST
// API. No gateway websocket is needed just to post messages.
package discord

import (
	"context"
	"fmt"
	"strconv"

	"github.com/bwmarrin/discordgo"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

const embedColor = 0x3fbc58

type Notifier struct {
	session   *discordgo.Session
	channelID string
}

func New(botToken, channelID string) (*Notifier, error) {
	s, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, fmt.Errorf("discord session: %w", err)
	}
	return &Notifier{session: s, channelID: channelID}, nil
}

func (n *Notifier) Name() string { return "discord" }

func (n *Notifier) NotifyText(ctx context.Context, msg string) error {
	_, err := n.session.ChannelMessageSend(n.channelID, msg, discordgo.WithContext(ctx))
	return err
}

func (n *Notifier) NotifyListing(ctx context.Context, l events.ListingDiscovered) error {
	_, err := n.session.ChannelMessageSendEmbed(n.channelID, embed(l), discordgo.WithContext(ctx))
	return err
}

func embed(l events.ListingDiscovered) *discordgo.MessageEmbed {
	e := &discordgo.MessageEmbed{
		Title: "🏠 New Advertisement",
		Color: embedColor,
	}

	if l.Title != "" {
		e.Description = l.Title
	}
	if l.URL != "" {
		if e.Description != "" {
			e.Description += "\n"
		}
		e.Description += fmt.Sprintf("[📎 View on %s](%s)", l.Source, l.URL)
	}

	add := func(name, value string, inline bool) {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: name, Value: value, Inline: inline})
	}
	if l.Category != "" {
		add("📍 Location", l.Category, false)
	}
	if l.Price != "" {
		add("💰 Price", l.Price, true)
	}
	if l.Rooms != nil && *l.Rooms != 0 {
		add("🚪 Rooms", strconv.Itoa(*l.Rooms), true)
	}
	if l.AreaM2 != nil && *l.AreaM2 != 0 {
		add("📐 Area", fmt.Sprintf("%d m²", *l.AreaM2), true)
	}
	if l.Series != "" {
		add("🏢 Series", l.Series, true)
	}
	if l.ImageURL != "" {
		e.Image = &discordgo.MessageEmbedImage{URL: l.ImageURL}
	}
	if l.ExternalID != "" {
		e.Footer = &discordgo.MessageEmbedFooter{Text: "ID: " + l.ExternalID}
	}
	return e
}
