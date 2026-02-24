package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/n0madic/graylog-mcp/config"
	"github.com/n0madic/graylog-mcp/graylog"
)

func TestValidateGraylogURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "https", input: "https://graylog.example.com", wantErr: false},
		{name: "http", input: "http://graylog.example.com", wantErr: false},
		{name: "with port", input: "https://graylog.example.com:9000", wantErr: false},
		{name: "invalid scheme", input: "ftp://graylog.example.com", wantErr: true},
		{name: "empty", input: "", wantErr: true},
		{name: "not url", input: "not-a-url", wantErr: true},
		{name: "missing host", input: "https:///api", wantErr: true},
		{name: "userinfo", input: "https://user:pass@graylog.example.com", wantErr: true},
		{name: "fragment", input: "https://graylog.example.com/#frag", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGraylogURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGraylogURL(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateGraylogOverrideURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "public ip", input: "https://8.8.8.8", wantErr: false},
		{name: "loopback", input: "http://127.0.0.1", wantErr: true},
		{name: "private", input: "http://10.0.0.1", wantErr: true},
		{name: "link-local", input: "http://169.254.1.1", wantErr: true},
		{name: "unspecified", input: "http://0.0.0.0", wantErr: true},
		{name: "localhost hostname", input: "http://localhost", wantErr: true},
		{name: "ipv6 loopback", input: "http://[::1]", wantErr: true},
		{name: "ipv6 private", input: "http://[fd00::1]", wantErr: true},
		{name: "cgnat", input: "http://100.100.1.1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGraylogOverrideURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGraylogOverrideURL(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestClientFromAuthHeader(t *testing.T) {
	baseClient := graylog.NewClient("", "", "", false, 30*time.Second)
	graylogURL := "https://graylog.example.com"

	// Bearer token — valid
	c := clientFromAuthHeader("Bearer mytoken", graylogURL, baseClient)
	if c == nil {
		t.Error("expected non-nil client for valid Bearer token")
	}

	// Bearer token — lowercase scheme should be accepted
	c = clientFromAuthHeader("bearer mytoken", graylogURL, baseClient)
	if c == nil {
		t.Error("expected non-nil client for lowercase bearer scheme")
	}

	// Bearer token — empty token
	c = clientFromAuthHeader("Bearer ", graylogURL, baseClient)
	if c != nil {
		t.Error("expected nil client for empty Bearer token")
	}

	// Basic auth — valid base64
	encoded := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	c = clientFromAuthHeader("Basic "+encoded, graylogURL, baseClient)
	if c == nil {
		t.Error("expected non-nil client for valid Basic auth")
	}

	// Basic auth — mixed case scheme should be accepted
	c = clientFromAuthHeader("bAsIc "+encoded, graylogURL, baseClient)
	if c == nil {
		t.Error("expected non-nil client for mixed-case Basic scheme")
	}

	// Basic auth — invalid base64
	c = clientFromAuthHeader("Basic not-valid-base64!!!", graylogURL, baseClient)
	if c != nil {
		t.Error("expected nil client for invalid base64")
	}

	// Basic auth — missing username (only colon)
	encodedEmpty := base64.StdEncoding.EncodeToString([]byte(":password"))
	c = clientFromAuthHeader("Basic "+encodedEmpty, graylogURL, baseClient)
	if c != nil {
		t.Error("expected nil client when username is empty")
	}

	// Unknown scheme
	c = clientFromAuthHeader("Digest something", graylogURL, baseClient)
	if c != nil {
		t.Error("expected nil client for unknown auth scheme")
	}

	// Empty header
	c = clientFromAuthHeader("", graylogURL, baseClient)
	if c != nil {
		t.Error("expected nil client for empty auth header")
	}
}

func TestAuthMiddlewareRejectsPrivateLoopbackAndLinkLocalOverrides(t *testing.T) {
	cfg := &config.Config{GraylogURL: "https://8.8.8.8"}
	baseClient := graylog.NewClient("", "", "", false, 2*time.Second)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := authMiddleware(cfg, baseClient)(next)

	tests := []struct {
		name        string
		overrideURL string
	}{
		{name: "loopback", overrideURL: "http://127.0.0.1"},
		{name: "private", overrideURL: "http://10.1.2.3"},
		{name: "link-local", overrideURL: "http://169.254.1.2"},
		{name: "ipv6 loopback", overrideURL: "http://[::1]"},
		{name: "cgnat", overrideURL: "http://100.64.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer token")
			req.Header.Set("X-Graylog-URL", tt.overrideURL)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s override, got %d", tt.overrideURL, rr.Code)
			}
		})
	}
}
