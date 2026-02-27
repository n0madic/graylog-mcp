package tools

import (
	"testing"

	"github.com/n0madic/graylog-mcp/graylog"
)

func TestTemplateizeMessagesEmpty(t *testing.T) {
	results, err := templateizeMessages(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil for empty input, got %v", results)
	}
}

func TestTemplateizeMessagesGroupsSimilar(t *testing.T) {
	messages := []graylog.MessageWrapper{
		{Message: graylog.Message{ID: "id-1", Message: "Connection to 10.0.0.1 failed: timeout"}, Index: "idx"},
		{Message: graylog.Message{ID: "id-2", Message: "Connection to 10.0.0.2 failed: timeout"}, Index: "idx"},
		{Message: graylog.Message{ID: "id-3", Message: "Connection to 10.0.0.3 failed: timeout"}, Index: "idx"},
		{Message: graylog.Message{ID: "id-4", Message: "User admin logged in from 192.168.1.1"}, Index: "idx"},
		{Message: graylog.Message{ID: "id-5", Message: "User root logged in from 192.168.1.2"}, Index: "idx"},
	}

	results, err := templateizeMessages(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one template, got none")
	}

	// Verify total count across all templates equals input count.
	totalCount := 0
	for _, r := range results {
		totalCount += r.Count
	}
	if totalCount != 5 {
		t.Fatalf("expected total count=5 across all templates, got %d", totalCount)
	}

	// First template should be the most frequent one (3 connection messages).
	if results[0].Count < 2 {
		t.Fatalf("expected first template to have count >= 2, got %d", results[0].Count)
	}
}

func TestTemplateizeMessagesNewlines(t *testing.T) {
	messages := []graylog.MessageWrapper{
		{Message: graylog.Message{ID: "id-1", Message: "line one\nline two\nline three"}, Index: "idx"},
	}

	results, err := templateizeMessages(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Multi-line message should be treated as a single entry.
	totalCount := 0
	for _, r := range results {
		totalCount += r.Count
	}
	if totalCount != 1 {
		t.Fatalf("expected total count=1 for single multi-line message, got %d", totalCount)
	}
}

func TestCapTemplateMessageIDs(t *testing.T) {
	results := []TemplateResult{
		{Template: "t1", Count: 10, MessageIDs: []string{"a", "b", "c", "d", "e", "f", "g"}},
		{Template: "t2", Count: 2, MessageIDs: []string{"x", "y"}},
	}

	capTemplateMessageIDs(results, 3)

	if len(results[0].MessageIDs) != 3 {
		t.Fatalf("expected 3 message IDs after cap, got %d", len(results[0].MessageIDs))
	}
	if len(results[1].MessageIDs) != 2 {
		t.Fatalf("expected 2 message IDs (unchanged), got %d", len(results[1].MessageIDs))
	}
}
