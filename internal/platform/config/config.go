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
		MaxPages:       envInt("MAX_PAGES", 3),
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
	UsersFile      string // optional; seeded on start when set
	FeedCollapseAt int
}

func MatcherFromEnv() (Matcher, error) {
	return Matcher{
		Common:         commonFromEnv(),
		UsersFile:      os.Getenv("USERS_FILE"),
		FeedCollapseAt: envInt("FEED_COLLAPSE_AT", 3),
	}, nil
}

type Notifier struct {
	Common
	DiscordBotToken string
	// Delay is the pause after each message, to stay under Discord's rate limit.
	Delay time.Duration
}

func NotifierFromEnv() (Notifier, error) {
	c := Notifier{
		Common:          commonFromEnv(),
		DiscordBotToken: os.Getenv("DISCORD_BOT_TOKEN"),
		Delay:           time.Duration(envInt("NOTIFY_DELAY_MS", 1000)) * time.Millisecond,
	}
	if c.DiscordBotToken == "" {
		return c, errors.New("DISCORD_BOT_TOKEN is required")
	}
	return c, nil
}

type Admin struct {
	Common
	UsersFile      string
	FeedCollapseAt int
	Retention      time.Duration
}

func AdminFromEnv() (Admin, error) {
	c := Admin{
		Common:         commonFromEnv(),
		UsersFile:      env("USERS_FILE", "config/users.yaml"),
		FeedCollapseAt: envInt("FEED_COLLAPSE_AT", 3),
		Retention:      time.Duration(envInt("RETENTION_HOURS", 7*24)) * time.Hour,
	}
	if c.Retention <= 0 {
		return c, errors.New("RETENTION_HOURS must be positive")
	}
	return c, nil
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
