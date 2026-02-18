package main

import (
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"github.com/n0madic/graylog-mcp/config"
	"github.com/n0madic/graylog-mcp/graylog"
	"github.com/n0madic/graylog-mcp/tools"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	var client *graylog.Client
	if cfg.Token != "" {
		client = graylog.NewClient(cfg.GraylogURL, cfg.Token, "token", cfg.TLSSkipVerify, cfg.Timeout)
	} else {
		client = graylog.NewClient(cfg.GraylogURL, cfg.Username, cfg.Password, cfg.TLSSkipVerify, cfg.Timeout)
	}

	s := server.NewMCPServer(
		"graylog-mcp",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	tools.RegisterAll(s, client)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
