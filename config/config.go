package config

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	GraylogURL    string
	Username      string
	Password      string
	Token         string
	TLSSkipVerify bool
	Timeout       time.Duration
	Transport     string // "stdio" or "http"
	Bind          string // HTTP listen address, e.g. "0.0.0.0:8090"
}

func Load() (*Config, error) {
	cfg := &Config{}

	flag.StringVar(&cfg.GraylogURL, "url", os.Getenv("GRAYLOG_URL"), "Graylog base URL")
	flag.StringVar(&cfg.Username, "username", os.Getenv("GRAYLOG_USERNAME"), "Graylog username")
	flag.StringVar(&cfg.Password, "password", os.Getenv("GRAYLOG_PASSWORD"), "Graylog password")
	flag.StringVar(&cfg.Token, "token", os.Getenv("GRAYLOG_TOKEN"), "Graylog API access token (alternative to username/password)")
	var tlsSkipVerifyDefault bool
	if v := os.Getenv("GRAYLOG_TLS_SKIP_VERIFY"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid GRAYLOG_TLS_SKIP_VERIFY %q: must be true/false/1/0", v)
		}
		tlsSkipVerifyDefault = parsed
	}
	flag.BoolVar(&cfg.TLSSkipVerify, "tls-skip-verify", tlsSkipVerifyDefault, "Skip TLS certificate verification")

	transportDefault := os.Getenv("GRAYLOG_MCP_TRANSPORT")
	if transportDefault == "" {
		transportDefault = "stdio"
	}
	flag.StringVar(&cfg.Transport, "transport", transportDefault, `Transport type: "stdio" or "http"`)

	bindDefault := os.Getenv("GRAYLOG_MCP_HTTP_BIND")
	if bindDefault == "" {
		bindDefault = "0.0.0.0:8090"
	}
	flag.StringVar(&cfg.Bind, "bind", bindDefault, `HTTP listen address (http transport only), e.g. "0.0.0.0:8090"`)

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

	if cfg.Transport != "stdio" && cfg.Transport != "http" {
		return nil, fmt.Errorf("invalid transport %q: must be \"stdio\" or \"http\"", cfg.Transport)
	}

	// In http transport, GRAYLOG_URL can be omitted and supplied per-request via X-Graylog-URL header.
	if cfg.GraylogURL == "" && cfg.Transport == "stdio" {
		return nil, fmt.Errorf("GRAYLOG_URL is required (env or --url flag)")
	}

	if cfg.GraylogURL != "" {
		parsedURL, err := url.Parse(cfg.GraylogURL)
		if err != nil {
			return nil, fmt.Errorf("invalid GRAYLOG_URL: %w", err)
		}
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return nil, fmt.Errorf("GRAYLOG_URL must use http or https scheme, got %q", parsedURL.Scheme)
		}
	}

	if cfg.TLSSkipVerify {
		fmt.Fprintf(os.Stderr, "WARNING: TLS certificate verification is disabled. Credentials may be vulnerable to interception.\n")
	}

	// In http transport, credentials are provided per-request via Authorization header.
	// In stdio transport, static credentials are required at startup.
	if cfg.Transport == "http" && (cfg.Token != "" || cfg.Username != "" || cfg.Password != "") {
		fmt.Fprintf(os.Stderr, "WARNING: Graylog token or username/password are ignored in http transport mode; credentials are provided per-request via the Authorization header.\n")
	}
	if cfg.Transport == "stdio" {
		hasToken := cfg.Token != ""
		hasCredentials := cfg.Username != "" && cfg.Password != ""
		if !hasToken && !hasCredentials {
			return nil, fmt.Errorf("authentication required: set GRAYLOG_TOKEN (env or --token flag) or both GRAYLOG_USERNAME and GRAYLOG_PASSWORD")
		}
	}

	return cfg, nil
}
