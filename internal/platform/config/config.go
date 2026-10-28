// Package config loads process configuration from the environment.
package config

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DiscordBotToken  string
	DiscordChannelID string

	DatabaseURL string

	SSURL          string
	SSFormData     string // raw "k=v&k2=v2", values unencoded
	CrawlInterval  time.Duration
	RequestTimeout time.Duration
	NotifyDelay    time.Duration
}

func FromEnv() (Config, error) {
	c := Config{
		DiscordBotToken:  os.Getenv("DISCORD_BOT_TOKEN"),
		DiscordChannelID: os.Getenv("DISCORD_CHANNEL_ID"),
		SSURL:            env("SS_URL", "https://www.ss.com/lv/real-estate/flats/search-result/"),
		SSFormData:       os.Getenv("SS_FORM_DATA"),
		CrawlInterval:    time.Duration(envInt("CRAWL_INTERVAL_MINUTES", 5)) * time.Minute,
		RequestTimeout:   time.Duration(envInt("REQUEST_TIMEOUT", 30)) * time.Second,
		NotifyDelay:      time.Second,
	}

	if v := os.Getenv("DATABASE_URL"); v != "" {
		c.DatabaseURL = v
	} else {
		u := url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(env("DB_USER", "admonitorbot"), os.Getenv("DB_PASSWORD")),
			Host:   env("DB_HOST", "localhost") + ":" + env("DB_PORT", "5432"),
			Path:   env("DB_NAME", "admonitorbot"),
		}
		c.DatabaseURL = u.String()
	}

	return c, c.validate()
}

func (c Config) validate() error {
	var errs []error
	if c.DiscordBotToken == "" {
		errs = append(errs, errors.New("DISCORD_BOT_TOKEN is required"))
	}
	if c.DiscordChannelID == "" {
		errs = append(errs, errors.New("DISCORD_CHANNEL_ID is required"))
	}
	if c.CrawlInterval <= 0 {
		errs = append(errs, errors.New("CRAWL_INTERVAL_MINUTES must be positive"))
	}
	if c.RequestTimeout <= 0 {
		errs = append(errs, errors.New("REQUEST_TIMEOUT must be positive"))
	}
	return errors.Join(errs...)
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
