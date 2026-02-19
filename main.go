package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/server"
	"github.com/n0madic/graylog-mcp/config"
	"github.com/n0madic/graylog-mcp/graylog"
	"github.com/n0madic/graylog-mcp/tools"
)

type contextKey string

// ClientContextKey is the context key used to store a per-request Graylog client.
// Used by HTTP transport to pass credentials extracted from the Authorization header.
const clientContextKey contextKey = "graylog-client"

// ClientFromContext returns the Graylog client stored in ctx, or nil if none.
func clientFromContext(ctx context.Context) *graylog.Client {
	c, _ := ctx.Value(clientContextKey).(*graylog.Client)
	return c
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	s := server.NewMCPServer(
		"graylog-mcp",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	if cfg.Transport == "http" {
		// HTTP mode: credentials are provided per-request via the Authorization header.
		// The auth middleware injects a graylog.Client into the request context before
		// the MCP server sees the request. The LLM only ever sees tool results.
		tools.RegisterAll(s, clientFromContext)

		httpSrv := server.NewStreamableHTTPServer(s,
			server.WithEndpointPath("/mcp"),
			server.WithStateLess(true),
		)

		fmt.Fprintf(os.Stderr, "Graylog MCP server listening on %s (Streamable HTTP /mcp)\n", cfg.Bind)
		fmt.Fprintf(os.Stderr, "WARNING: HTTP transport runs without TLS. Authorization headers are transmitted in plaintext. Use a TLS-terminating reverse proxy in production.\n")

		if err := http.ListenAndServe(cfg.Bind, authMiddleware(cfg)(httpSrv)); err != nil {
			fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// stdio mode: static client from startup credentials.
	var client *graylog.Client
	if cfg.Token != "" {
		client = graylog.NewClient(cfg.GraylogURL, cfg.Token, "token", cfg.TLSSkipVerify, cfg.Timeout)
	} else {
		client = graylog.NewClient(cfg.GraylogURL, cfg.Username, cfg.Password, cfg.TLSSkipVerify, cfg.Timeout)
	}

	tools.RegisterAll(s, func(_ context.Context) *graylog.Client { return client })

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

// writeJSONError writes a JSON error response. The message is JSON-encoded to
// prevent injection of special characters (", \, newlines) from untrusted input.
func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	b, _ := json.Marshal(map[string]string{"error": msg})
	w.Write(b) //nolint:errcheck
}

// authMiddleware resolves the Graylog URL and credentials from request headers and
// injects a per-request *graylog.Client into the context. The MCP server and LLM
// never see credentials or the target URL — both are fully transparent to the protocol.
//
// Headers:
//
//	X-Graylog-URL:  https://graylog.example.com   (overrides GRAYLOG_URL; optional if server has GRAYLOG_URL set)
//	Authorization:  Bearer <graylog_api_token>
//	Authorization:  Basic base64(username:password)
func authMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			graylogURL := r.Header.Get("X-Graylog-URL")
			if graylogURL == "" {
				graylogURL = cfg.GraylogURL
			}
			if graylogURL == "" {
				writeJSONError(w, "Graylog URL required", http.StatusBadRequest)
				return
			}
			if err := validateGraylogURL(graylogURL); err != nil {
				writeJSONError(w, "invalid X-Graylog-URL: "+err.Error(), http.StatusBadRequest)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeJSONError(w, "Authorization header required", http.StatusUnauthorized)
				return
			}
			client := clientFromAuthHeader(authHeader, graylogURL, cfg)
			if client == nil {
				writeJSONError(w, "invalid Authorization header: use Bearer <token> or Basic base64(user:pass)", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), clientContextKey, client)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func validateGraylogURL(raw string) error {
	p, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if p.Scheme != "http" && p.Scheme != "https" {
		return fmt.Errorf("must use http or https scheme, got %q", p.Scheme)
	}
	return nil
}

// clientFromAuthHeader builds a graylog.Client from an Authorization header value.
// Bearer tokens use Graylog's token auth convention (Basic token_value:"token").
func clientFromAuthHeader(authHeader, graylogURL string, cfg *config.Config) *graylog.Client {
	switch {
	case strings.HasPrefix(authHeader, "Bearer "):
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" {
			return nil
		}
		return graylog.NewClient(graylogURL, token, "token", cfg.TLSSkipVerify, cfg.Timeout)

	case strings.HasPrefix(authHeader, "Basic "):
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authHeader, "Basic "))
		if err != nil {
			return nil
		}
		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) != 2 || parts[0] == "" {
			return nil
		}
		return graylog.NewClient(graylogURL, parts[0], parts[1], cfg.TLSSkipVerify, cfg.Timeout)
	}
	return nil
}
