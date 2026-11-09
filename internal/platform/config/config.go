// Package config loads process configuration from the environment. Every
// service has its own struct so it only requires the variables it uses.
package config

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Common is what every service needs.
type Common struct {
	DatabaseURL  string
	KafkaBrokers []string
	// Namespace prefixes topic and consumer-group names, so several
	// environments (or test runs) can share one Kafka cluster.
	Namespace string
}

// Topic is the topic name to use for name in this environment.
func (c Common) Topic(name string) string { return c.prefix(name) }

// Group is the consumer group to use for name in this environment.
func (c Common) Group(name string) string { return c.prefix(name) }

func (c Common) prefix(name string) string {
	if c.Namespace == "" {
		return name
	}
	return c.Namespace + "." + name
}

func commonFromEnv() Common {
	c := Common{KafkaBrokers: strings.Split(env("KAFKA_BROKERS", "localhost:9092"), ","), Namespace: os.Getenv("NAMESPACE")}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		c.DatabaseURL = v
		return c
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(env("DB_USER", "admonitorbot"), os.Getenv("DB_PASSWORD")),
		Host:   env("DB_HOST", "localhost") + ":" + env("DB_PORT", "5432"),
		Path:   env("DB_NAME", "admonitorbot"),
	}
	c.DatabaseURL = u.String()
	return c
}

type Scraper struct {
	Common
	Source         string
	CrawlInterval  time.Duration
	RequestTimeout time.Duration
	// RequestGap is the minimum pause between two requests to the site, so a
	// cycle with several feeds never bursts.
	RequestGap time.Duration
	// MaxPages caps how deep one feed is followed in a cycle.
	MaxPages       int
	FeedCollapseAt int
	// AdminChannel receives start/stop/error messages; empty target disables them.
	AdminChannel AdminChannel
}

type AdminChannel struct{ Kind, Target string }

func ScraperFromEnv() (Scraper, error) {
	c := Scraper{
		Common:         commonFromEnv(),
		Source:         "ss.com",
		CrawlInterval:  time.Duration(envInt("CRAWL_INTERVAL_MINUTES", 5)) * time.Minute,
		RequestTimeout: time.Duration(envInt("REQUEST_TIMEOUT", 30)) * time.Second,
		RequestGap:     time.Duration(envInt("REQUEST_GAP_SECONDS", 2)) * time.Second,
		MaxPages:       envInt("MAX_PAGES", 5),
		FeedCollapseAt: envInt("FEED_COLLAPSE_AT", 3),
		AdminChannel:   AdminChannel{Kind: env("ADMIN_CHANNEL_KIND", "discord"), Target: os.Getenv("ADMIN_CHANNEL_ID")},
	}
	var errs []error
	if c.CrawlInterval <= 0 {
		errs = append(errs, errors.New("CRAWL_INTERVAL_MINUTES must be positive"))
	}
	if c.RequestTimeout <= 0 {
		errs = append(errs, errors.New("REQUEST_TIMEOUT must be positive"))
	}
	if c.MaxPages < 1 {
		errs = append(errs, errors.New("MAX_PAGES must be at least 1"))
	}
	return c, errors.Join(errs...)
}

type Ingest struct {
	Common
	// SeedWindow is how long a newly seen feed is stored without notifying.
	SeedWindow time.Duration
}

func IngestFromEnv() (Ingest, error) {
	c := Ingest{Common: commonFromEnv(), SeedWindow: time.Duration(envInt("SEED_WINDOW_SECONDS", 120)) * time.Second}
	if c.SeedWindow <= 0 {
		return c, errors.New("SEED_WINDOW_SECONDS must be positive")
	}
	return c, nil
}

type Matcher struct {
	Common
	FeedCollapseAt int
	// RequestTimeout and RequestGap apply to ad pages the matcher reads when a
	// search filters on something the feed list does not show.
	RequestTimeout time.Duration
	RequestGap     time.Duration
}

func MatcherFromEnv() (Matcher, error) {
	c := Matcher{
		Common:         commonFromEnv(),
		FeedCollapseAt: envInt("FEED_COLLAPSE_AT", 3),
		RequestTimeout: time.Duration(envInt("REQUEST_TIMEOUT", 30)) * time.Second,
		RequestGap:     time.Duration(envInt("REQUEST_GAP_SECONDS", 2)) * time.Second,
	}
	if c.RequestTimeout <= 0 {
		return c, errors.New("REQUEST_TIMEOUT must be positive")
	}
	return c, nil
}

type Notifier struct {
	Common
	DiscordBotToken string
	// Delay is the pause after each message, to stay under Discord's rate limit.
	Delay time.Duration
	// Burst is how many ad messages one search may send per BurstWindow; the
	// rest are sent as one digest when the window is over.
	Burst       int
	BurstWindow time.Duration
}

func NotifierFromEnv() (Notifier, error) {
	c := Notifier{
		Common:          commonFromEnv(),
		DiscordBotToken: os.Getenv("DISCORD_BOT_TOKEN"),
		Delay:           time.Duration(envInt("NOTIFY_DELAY_MS", 1000)) * time.Millisecond,
		Burst:           envInt("NOTIFY_BURST", 5),
		BurstWindow:     time.Duration(envInt("NOTIFY_BURST_WINDOW_MINUTES", 10)) * time.Minute,
	}
	if c.DiscordBotToken == "" {
		return c, errors.New("DISCORD_BOT_TOKEN is required")
	}
	if c.Burst < 1 || c.BurstWindow <= 0 {
		return c, errors.New("NOTIFY_BURST and NOTIFY_BURST_WINDOW_MINUTES must be positive")
	}
	return c, nil
}

type Admin struct {
	Common
	FeedCollapseAt int
	Retention      time.Duration
}

func AdminFromEnv() (Admin, error) {
	c := Admin{
		Common:         commonFromEnv(),
		FeedCollapseAt: envInt("FEED_COLLAPSE_AT", 3),
		Retention:      time.Duration(envInt("RETENTION_HOURS", 60*24)) * time.Hour,
	}
	if c.Retention <= 0 {
		return c, errors.New("RETENTION_HOURS must be positive")
	}
	return c, nil
}

// SelfService is how users manage their own searches (bot and dashboard).
type SelfService struct {
	// MaxSearches per user; admins are exempt. 0 = unlimited.
	MaxSearches int
	// Admins may see and edit everyone's searches: chat accounts written
	// "platform:id", e.g. "discord:123456789012345678".
	Admins []string
	// PublicURL is where people open the dashboard, e.g.
	// https://ads.example.com; login links point there.
	PublicURL string
}

func selfServiceFromEnv() SelfService {
	var admins []string
	for _, a := range strings.Split(os.Getenv("ADMIN_USERS"), ",") {
		if a = strings.TrimSpace(a); a != "" {
			admins = append(admins, a)
		}
	}
	return SelfService{
		MaxSearches: envInt("MAX_SEARCHES_PER_USER", 10),
		Admins:      admins,
		PublicURL:   strings.TrimRight(env("PUBLIC_URL", "http://127.0.0.1:8080"), "/"),
	}
}

// Bot is the chat bot users talk to (/watch, /searches, /dashboard). Each
// platform with a token is served; Discord for now.
type Bot struct {
	Common
	SelfService
	DiscordBotToken string
	// GuildID registers the commands on one server only: they update
	// instantly there, while global commands can take a while.
	GuildID        string
	FeedCollapseAt int
}

func BotFromEnv() (Bot, error) {
	c := Bot{
		Common: commonFromEnv(), SelfService: selfServiceFromEnv(),
		DiscordBotToken: os.Getenv("DISCORD_BOT_TOKEN"), GuildID: os.Getenv("DISCORD_GUILD_ID"),
		FeedCollapseAt: envInt("FEED_COLLAPSE_AT", 3),
	}
	if c.DiscordBotToken == "" {
		return c, errors.New("DISCORD_BOT_TOKEN is required")
	}
	return c, nil
}

// API serves the dashboard and its JSON API.
type API struct {
	Common
	SelfService
	Addr           string // listen address
	WebDir         string // built dashboard (web/dist); empty = API only
	FeedCollapseAt int
	// RequestGap spaces out the ad pages an admin asks the dashboard to read.
	RequestGap time.Duration
}

func APIFromEnv() (API, error) {
	return API{
		Common: commonFromEnv(), SelfService: selfServiceFromEnv(),
		Addr: env("API_ADDR", "127.0.0.1:8080"), WebDir: env("WEB_DIR", "web/dist"),
		FeedCollapseAt: envInt("FEED_COLLAPSE_AT", 3),
		RequestGap:     time.Duration(envInt("REQUEST_GAP_SECONDS", 2)) * time.Second,
	}, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return def
	}
	return v
}
