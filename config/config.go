package config

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"
)

type Config struct {
	GraylogURL    string
	Username      string
	Password      string
	Token         string
	TLSSkipVerify bool
	Timeout       time.Duration
}

func Load() (*Config, error) {
	cfg := &Config{}

	flag.StringVar(&cfg.GraylogURL, "url", os.Getenv("GRAYLOG_URL"), "Graylog base URL")
	flag.StringVar(&cfg.Username, "username", os.Getenv("GRAYLOG_USERNAME"), "Graylog username")
	flag.StringVar(&cfg.Password, "password", os.Getenv("GRAYLOG_PASSWORD"), "Graylog password")
	flag.StringVar(&cfg.Token, "token", os.Getenv("GRAYLOG_TOKEN"), "Graylog API access token (alternative to username/password)")
	flag.BoolVar(&cfg.TLSSkipVerify, "tls-skip-verify", os.Getenv("GRAYLOG_TLS_SKIP_VERIFY") == "true", "Skip TLS certificate verification")

	defaultTimeout := 30 * time.Second
	if t := os.Getenv("GRAYLOG_TIMEOUT"); t != "" {
		parsed, err := time.ParseDuration(t)
		if err != nil {
			return nil, fmt.Errorf("invalid GRAYLOG_TIMEOUT %q: %w", t, err)
		}
		defaultTimeout = parsed
	}
	flag.DurationVar(&cfg.Timeout, "timeout", defaultTimeout, "HTTP request timeout")

	flag.Parse()

	// Warn if secrets are passed via CLI flags (visible in process listings)
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "password" || f.Name == "token" {
			fmt.Fprintf(os.Stderr, "WARNING: --%s passed via CLI flag; visible in process listings. Prefer environment variables.\n", f.Name)
		}
	})

	if cfg.GraylogURL == "" {
		return nil, fmt.Errorf("GRAYLOG_URL is required (env or --url flag)")
	}

	parsedURL, err := url.Parse(cfg.GraylogURL)
	if err != nil {
		return nil, fmt.Errorf("invalid GRAYLOG_URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("GRAYLOG_URL must use http or https scheme, got %q", parsedURL.Scheme)
	}

	if cfg.TLSSkipVerify {
		fmt.Fprintf(os.Stderr, "WARNING: TLS certificate verification is disabled. Credentials may be vulnerable to interception.\n")
	}

	hasToken := cfg.Token != ""
	hasCredentials := cfg.Username != "" && cfg.Password != ""

	if !hasToken && !hasCredentials {
		return nil, fmt.Errorf("authentication required: set GRAYLOG_TOKEN (env or --token flag) or both GRAYLOG_USERNAME and GRAYLOG_PASSWORD")
	}

	return cfg, nil
}
