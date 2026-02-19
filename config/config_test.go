package config_test

import (
	"flag"
	"os"
	"testing"

	"github.com/n0madic/graylog-mcp/config"
)

// setupConfigTest resets the global flag state and strips test flags from os.Args
// so that config.Load() can call flag.Parse() cleanly in each test.
func setupConfigTest(t *testing.T) {
	t.Helper()
	origArgs := os.Args
	os.Args = os.Args[:1] // keep only the program name, drop test flags
	t.Cleanup(func() { os.Args = origArgs })
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
}

func TestLoad_InvalidTLSSkipVerify(t *testing.T) {
	setupConfigTest(t)
	t.Setenv("GRAYLOG_TLS_SKIP_VERIFY", "yes")
	_, err := config.Load()
	if err == nil {
		t.Error("expected error for invalid GRAYLOG_TLS_SKIP_VERIFY value 'yes'")
	}
}

func TestLoad_InvalidTimeout(t *testing.T) {
	setupConfigTest(t)
	t.Setenv("GRAYLOG_TIMEOUT", "not-a-duration")
	_, err := config.Load()
	if err == nil {
		t.Error("expected error for invalid GRAYLOG_TIMEOUT value")
	}
}

func TestLoad_StdioMissingCredentials(t *testing.T) {
	setupConfigTest(t)
	t.Setenv("GRAYLOG_URL", "https://graylog.example.com")
	t.Setenv("GRAYLOG_MCP_TRANSPORT", "stdio")
	t.Setenv("GRAYLOG_TOKEN", "")
	t.Setenv("GRAYLOG_USERNAME", "")
	t.Setenv("GRAYLOG_PASSWORD", "")
	t.Setenv("GRAYLOG_TLS_SKIP_VERIFY", "")
	t.Setenv("GRAYLOG_TIMEOUT", "")

	_, err := config.Load()
	if err == nil {
		t.Error("expected error when stdio transport has no credentials")
	}
}

func TestLoad_HTTPTransportNoURL(t *testing.T) {
	setupConfigTest(t)
	t.Setenv("GRAYLOG_MCP_TRANSPORT", "http")
	t.Setenv("GRAYLOG_URL", "")
	t.Setenv("GRAYLOG_TOKEN", "")
	t.Setenv("GRAYLOG_USERNAME", "")
	t.Setenv("GRAYLOG_PASSWORD", "")
	t.Setenv("GRAYLOG_TLS_SKIP_VERIFY", "")
	t.Setenv("GRAYLOG_TIMEOUT", "")

	_, err := config.Load()
	if err != nil {
		t.Errorf("http transport without URL should succeed, got: %v", err)
	}
}

func TestLoad_ValidTLSSkipVerify(t *testing.T) {
	for _, val := range []string{"true", "false", "1", "0", "TRUE", "FALSE"} {
		setupConfigTest(t)
		t.Setenv("GRAYLOG_TLS_SKIP_VERIFY", val)
		// Provide required fields so Load() succeeds past TLS check
		t.Setenv("GRAYLOG_URL", "https://graylog.example.com")
		t.Setenv("GRAYLOG_MCP_TRANSPORT", "stdio")
		t.Setenv("GRAYLOG_TOKEN", "mytoken")
		t.Setenv("GRAYLOG_TIMEOUT", "")

		_, err := config.Load()
		if err != nil {
			t.Errorf("GRAYLOG_TLS_SKIP_VERIFY=%q should be valid, got: %v", val, err)
		}
		// Reset flag CommandLine for next iteration
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	}
}
