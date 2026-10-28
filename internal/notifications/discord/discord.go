// Package discord delivers notifications to a Discord channel over the REST
// API. No gateway websocket is needed just to post messages.
package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
)

const embedColor = 0x3fbc58

// Notifier posts to any channel the bot can write to; the channel ID is
// passed per call so one bot token serves every user.
type Notifier struct {
	session *discordgo.Session
}

func New(botToken string) (*Notifier, error) {
	s, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, fmt.Errorf("discord session: %w", err)
	}
	return &Notifier{session: s}, nil
}

func (n *Notifier) Kind() string { return "discord" }

func (n *Notifier) NotifyText(ctx context.Context, channelID, msg string) error {
	_, err := n.session.ChannelMessageSend(channelID, msg, discordgo.WithContext(ctx))
	return err
}

func (n *Notifier) NotifyListing(ctx context.Context, channelID string, l events.ListingDiscovered) error {
	_, err := n.session.ChannelMessageSendEmbed(channelID, embed(l), discordgo.WithContext(ctx))
	return err
}

// embed renders a listing without knowing the source: the source decided which
// facts (Fields) are worth showing, in which order.
func embed(l events.ListingDiscovered) *discordgo.MessageEmbed {
	e := &discordgo.MessageEmbed{
		Title: "🆕 New listing",
		Color: embedColor,
	}

	e.Description = l.Title
	if l.URL != "" {
		if e.Description != "" {
			e.Description += "\n"
		}
		e.Description += fmt.Sprintf("[📎 View on %s](%s)", l.Source, l.URL)
	}

	if l.Price != "" {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "💰 Price", Value: l.Price, Inline: true})
	}
	for _, f := range l.Fields {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: f.Name, Value: f.Value, Inline: f.Inline})
	}
	if l.ImageURL != "" {
		e.Image = &discordgo.MessageEmbedImage{URL: l.ImageURL}
	}
	if l.ExternalID != "" {
		e.Footer = &discordgo.MessageEmbedFooter{Text: "ID: " + l.ExternalID}
	}
	return e
}
