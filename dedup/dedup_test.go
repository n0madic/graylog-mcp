package dedup

import (
	"testing"

	"github.com/n0madic/graylog-mcp/graylog"
)

func makeMsg(id, msg string) graylog.MessageWrapper {
	return graylog.MessageWrapper{
		Message: graylog.Message{ID: id, Message: msg, Source: "testhost"},
		Index:   "index-1",
	}
}

func TestDeduplicate_empty(t *testing.T) {
	results := Deduplicate(nil, nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestDeduplicate_allUnique(t *testing.T) {
	msgs := []graylog.MessageWrapper{
		makeMsg("1", "error one"),
		makeMsg("2", "error two"),
		makeMsg("3", "error three"),
	}
	results := Deduplicate(msgs, nil)
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Count != 1 {
			t.Errorf("expected count=1, got %d", r.Count)
		}
	}
}

func TestDeduplicate_allDuplicates(t *testing.T) {
	msgs := []graylog.MessageWrapper{
		makeMsg("1", "same message"),
		makeMsg("2", "same message"),
		makeMsg("3", "same message"),
	}
	results := Deduplicate(msgs, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Count != 3 {
		t.Errorf("expected count=3, got %d", results[0].Count)
	}
	if len(results[0].MessageIDs) != 3 {
		t.Errorf("expected 3 message IDs, got %d", len(results[0].MessageIDs))
	}
}

func TestDeduplicate_mixed(t *testing.T) {
	msgs := []graylog.MessageWrapper{
		makeMsg("1", "error A"),
		makeMsg("2", "error B"),
		makeMsg("3", "error A"),
		makeMsg("4", "error C"),
		makeMsg("5", "error B"),
	}
	results := Deduplicate(msgs, nil)
	if len(results) != 3 {
		t.Errorf("expected 3 unique groups, got %d", len(results))
	}

	// Count total occurrences
	total := 0
	for _, r := range results {
		total += r.Count
	}
	if total != 5 {
		t.Errorf("expected total count=5, got %d", total)
	}
}

func TestCapMessageIDs_capBelowLength(t *testing.T) {
	results := []DedupResult{
		{MessageIDs: []string{"a", "b", "c", "d", "e"}},
	}
	CapMessageIDs(results, 3)
	if len(results[0].MessageIDs) != 3 {
		t.Errorf("expected 3 message IDs after cap, got %d", len(results[0].MessageIDs))
	}
}

func TestCapMessageIDs_capAtLength(t *testing.T) {
	results := []DedupResult{
		{MessageIDs: []string{"a", "b", "c"}},
	}
	CapMessageIDs(results, 3)
	if len(results[0].MessageIDs) != 3 {
		t.Errorf("expected 3 message IDs unchanged, got %d", len(results[0].MessageIDs))
	}
}

func TestCapMessageIDs_capAboveLength(t *testing.T) {
	results := []DedupResult{
		{MessageIDs: []string{"a", "b"}},
	}
	CapMessageIDs(results, 10)
	if len(results[0].MessageIDs) != 2 {
		t.Errorf("expected 2 message IDs unchanged, got %d", len(results[0].MessageIDs))
	}
}

func TestHashMessage_stability(t *testing.T) {
	msg := graylog.Message{
		ID:        "some-id",
		Timestamp: "2024-01-01T00:00:00Z",
		Source:    "myhost",
		Message:   "test message",
	}
	h1 := hashMessage(msg, nil)
	h2 := hashMessage(msg, nil)
	if h1 != h2 {
		t.Errorf("hash must be stable: got %s and %s", h1, h2)
	}
}

func TestHashMessage_differentContent(t *testing.T) {
	msg1 := graylog.Message{Source: "myhost", Message: "error A"}
	msg2 := graylog.Message{Source: "myhost", Message: "error B"}

	h1 := hashMessage(msg1, nil)
	h2 := hashMessage(msg2, nil)
	if h1 == h2 {
		t.Error("different messages should produce different hashes")
	}
}

func TestHashMessage_ignoresIDAndTimestamp(t *testing.T) {
	msg1 := graylog.Message{ID: "id-1", Timestamp: "2024-01-01T00:00:00Z", Source: "host", Message: "msg"}
	msg2 := graylog.Message{ID: "id-2", Timestamp: "2024-06-15T12:00:00Z", Source: "host", Message: "msg"}

	h1 := hashMessage(msg1, nil)
	h2 := hashMessage(msg2, nil)
	if h1 != h2 {
		t.Error("hash should ignore _id and timestamp fields")
	}
}

func TestHashMessageDoesNotPanicOnNonMarshalableExtra(t *testing.T) {
	msg := graylog.Message{
		ID:      "id-1",
		Source:  "host",
		Message: "msg",
		Extra: map[string]any{
			"bad": func() {},
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("hashMessage panicked with non-marshable Extra: %v", r)
		}
	}()

	h := hashMessage(msg, nil)
	if h == "" {
		t.Fatal("expected non-empty hash")
	}
}
