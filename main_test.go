package main

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/n0madic/graylog-mcp/config"
)

func TestValidateGraylogURL(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"https://graylog.example.com", false},
		{"http://graylog.example.com", false},
		{"https://graylog.example.com:9000", false},
		{"ftp://graylog.example.com", true},
		{"", true},          // empty: scheme is ""
		{"not-a-url", true}, // no scheme
		{"://missing", true},
	}
	for _, tt := range tests {
		err := validateGraylogURL(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateGraylogURL(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
	}
}

func TestClientFromAuthHeader(t *testing.T) {
	cfg := &config.Config{TLSSkipVerify: false, Timeout: 30 * time.Second}
	graylogURL := "https://graylog.example.com"

	// Bearer token — valid
	c := clientFromAuthHeader("Bearer mytoken", graylogURL, cfg)
	if c == nil {
		t.Error("expected non-nil client for valid Bearer token")
	}

	// Bearer token — empty token
	c = clientFromAuthHeader("Bearer ", graylogURL, cfg)
	if c != nil {
		t.Error("expected nil client for empty Bearer token")
	}

	// Basic auth — valid base64
	encoded := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	c = clientFromAuthHeader("Basic "+encoded, graylogURL, cfg)
	if c == nil {
		t.Error("expected non-nil client for valid Basic auth")
	}

	// Basic auth — invalid base64
	c = clientFromAuthHeader("Basic not-valid-base64!!!", graylogURL, cfg)
	if c != nil {
		t.Error("expected nil client for invalid base64")
	}

	// Basic auth — missing username (only colon)
	encodedEmpty := base64.StdEncoding.EncodeToString([]byte(":password"))
	c = clientFromAuthHeader("Basic "+encodedEmpty, graylogURL, cfg)
	if c != nil {
		t.Error("expected nil client when username is empty")
	}

	// Unknown scheme
	c = clientFromAuthHeader("Digest something", graylogURL, cfg)
	if c != nil {
		t.Error("expected nil client for unknown auth scheme")
	}

	// Empty header
	c = clientFromAuthHeader("", graylogURL, cfg)
	if c != nil {
		t.Error("expected nil client for empty auth header")
	}
}
