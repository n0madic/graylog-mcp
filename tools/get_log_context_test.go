package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/n0madic/graylog-mcp/graylog"
)

type contextSearchCall struct {
	Limit int
	Order string
}

func TestGetLogContextDedupUsesOverfetchAndRemovesOverlap(t *testing.T) {
	var searchCalls []contextSearchCall

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/messages/test-index/target":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]any{
					"fields": map[string]any{
						"_id":       "target",
						"timestamp": "2024-01-01T00:00:00.000Z",
						"source":    "svc-target",
						"message":   "target message",
					},
				},
				"index": "test-index",
			})
		case "/api/views/search/sync":
			call, err := parseContextSearchCall(r)
			if err != nil {
				t.Fatalf("failed to parse search call: %v", err)
			}
			searchCalls = append(searchCalls, call)

			switch call.Order {
			case "DESC":
				writeViewsSearchResponse(w, 10, []testLogMessage{
					{ID: "target", Timestamp: "2024-01-01T00:00:00.000Z", Source: "svc", Message: "target message", Index: "idx"},
					{ID: "overlap", Timestamp: "2024-01-01T00:00:00.000Z", Source: "svc", Message: "overlap", Index: "idx"},
					{ID: "before-2", Timestamp: "2023-12-31T23:59:59.000Z", Source: "svc", Message: "before2", Index: "idx"},
					{ID: "before-1", Timestamp: "2023-12-31T23:59:58.000Z", Source: "svc", Message: "before1", Index: "idx"},
				})
			case "ASC":
				writeViewsSearchResponse(w, 10, []testLogMessage{
					{ID: "target", Timestamp: "2024-01-01T00:00:00.000Z", Source: "svc", Message: "target message", Index: "idx"},
					{ID: "overlap", Timestamp: "2024-01-01T00:00:00.000Z", Source: "svc", Message: "overlap", Index: "idx"},
					{ID: "after-1", Timestamp: "2024-01-01T00:00:01.000Z", Source: "svc", Message: "after1", Index: "idx"},
					{ID: "after-2", Timestamp: "2024-01-01T00:00:02.000Z", Source: "svc", Message: "after2", Index: "idx"},
					{ID: "after-3", Timestamp: "2024-01-01T00:00:03.000Z", Source: "svc", Message: "after3", Index: "idx"},
				})
			default:
				t.Fatalf("unexpected sort order %q", call.Order)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := graylog.NewClient(server.URL, "token", "token", false, 2*time.Second)
	handler := getLogContextHandler(client)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"message_id":  "target",
		"index":       "test-index",
		"before":      float64(3),
		"after":       float64(3),
		"deduplicate": true,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(searchCalls) != 2 {
		t.Fatalf("expected 2 search calls, got %d", len(searchCalls))
	}

	descLimit, ascLimit := 0, 0
	for _, call := range searchCalls {
		if call.Order == "DESC" {
			descLimit = call.Limit
		}
		if call.Order == "ASC" {
			ascLimit = call.Limit
		}
	}
	if descLimit != 10 || ascLimit != 10 {
		t.Fatalf("expected overfetch limits 10/10, got DESC=%d ASC=%d", descLimit, ascLimit)
	}

	payload := decodeToolResultJSON(t, result)

	beforeIDs := extractContextMessageIDs(t, payload, "messages_before")
	afterIDs := extractContextMessageIDs(t, payload, "messages_after")

	if !reflect.DeepEqual(beforeIDs, []string{"before-1", "before-2", "overlap"}) {
		t.Fatalf("unexpected messages_before ids: %#v", beforeIDs)
	}
	if !reflect.DeepEqual(afterIDs, []string{"after-1", "after-2", "after-3"}) {
		t.Fatalf("unexpected messages_after ids: %#v", afterIDs)
	}

	if contextIncomplete, _ := payload["context_incomplete"].(bool); contextIncomplete {
		t.Fatal("context_incomplete should be false when both sides are fully filled")
	}
}

func TestGetLogContextWithoutDedupSkipsOverfetchAndSignalsShortfall(t *testing.T) {
	var searchCalls []contextSearchCall

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/messages/test-index/target":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]any{
					"fields": map[string]any{
						"_id":       "target",
						"timestamp": "2024-01-01T00:00:00.000Z",
						"source":    "svc-target",
						"message":   "target message",
					},
				},
				"index": "test-index",
			})
		case "/api/views/search/sync":
			call, err := parseContextSearchCall(r)
			if err != nil {
				t.Fatalf("failed to parse search call: %v", err)
			}
			searchCalls = append(searchCalls, call)

			switch call.Order {
			case "DESC":
				writeViewsSearchResponse(w, 3, []testLogMessage{
					{ID: "target", Timestamp: "2024-01-01T00:00:00.000Z", Source: "svc", Message: "target message", Index: "idx"},
					{ID: "overlap", Timestamp: "2024-01-01T00:00:00.000Z", Source: "svc", Message: "overlap", Index: "idx"},
					{ID: "before-1", Timestamp: "2023-12-31T23:59:59.000Z", Source: "svc", Message: "before1", Index: "idx"},
				})
			case "ASC":
				writeViewsSearchResponse(w, 3, []testLogMessage{
					{ID: "target", Timestamp: "2024-01-01T00:00:00.000Z", Source: "svc", Message: "target message", Index: "idx"},
					{ID: "overlap", Timestamp: "2024-01-01T00:00:00.000Z", Source: "svc", Message: "overlap", Index: "idx"},
					{ID: "after-1", Timestamp: "2024-01-01T00:00:01.000Z", Source: "svc", Message: "after1", Index: "idx"},
				})
			default:
				t.Fatalf("unexpected sort order %q", call.Order)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := graylog.NewClient(server.URL, "token", "token", false, 2*time.Second)
	handler := getLogContextHandler(client)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"message_id":  "target",
		"index":       "test-index",
		"before":      float64(2),
		"after":       float64(2),
		"deduplicate": false,
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(searchCalls) != 2 {
		t.Fatalf("expected 2 search calls, got %d", len(searchCalls))
	}

	descLimit, ascLimit := 0, 0
	for _, call := range searchCalls {
		if call.Order == "DESC" {
			descLimit = call.Limit
		}
		if call.Order == "ASC" {
			ascLimit = call.Limit
		}
	}
	if descLimit != 3 || ascLimit != 3 {
		t.Fatalf("expected non-overfetch limits 3/3, got DESC=%d ASC=%d", descLimit, ascLimit)
	}

	payload := decodeToolResultJSON(t, result)

	beforeIDs := extractContextMessageIDs(t, payload, "messages_before")
	afterIDs := extractContextMessageIDs(t, payload, "messages_after")
	if !reflect.DeepEqual(beforeIDs, []string{"before-1", "overlap"}) {
		t.Fatalf("unexpected messages_before ids: %#v", beforeIDs)
	}
	if !reflect.DeepEqual(afterIDs, []string{"after-1"}) {
		t.Fatalf("unexpected messages_after ids: %#v", afterIDs)
	}

	if contextIncomplete, _ := payload["context_incomplete"].(bool); !contextIncomplete {
		t.Fatal("context_incomplete should be true when one side is underfilled")
	}
	// Numeric echo fields (before_requested etc.) are intentionally omitted from response
	for _, noiseField := range []string{"before_requested", "after_requested", "before_returned", "after_returned"} {
		if _, exists := payload[noiseField]; exists {
			t.Fatalf("%s should not be present in response", noiseField)
		}
	}
}

func TestFitContextResultUsesLastResortForOversizedPayload(t *testing.T) {
	hugeBlob := strings.Repeat("x", 10000)
	result := map[string]any{
		"target_message": &graylog.MessageWrapper{
			Message: graylog.Message{
				ID:        "target-id",
				Timestamp: "2024-01-01T00:00:00.000Z",
				Source:    "svc",
				Message:   "target message",
				Extra: map[string]any{
					"blob": hugeBlob,
				},
			},
			Index: "test-index",
		},
		"messages_before":    []graylog.MessageWrapper{},
		"messages_after":     []graylog.MessageWrapper{},
		"context_incomplete": true,
	}

	toolResult, err := fitContextResult(result, 200)
	if err != nil {
		t.Fatalf("fitContextResult returned error: %v", err)
	}

	payload := decodeToolResultJSON(t, toolResult)
	if payload["target_message_id"] != "target-id" {
		t.Fatalf("unexpected target_message_id: %v", payload["target_message_id"])
	}
	if truncated, _ := payload["response_truncated"].(bool); !truncated {
		t.Fatal("expected response_truncated=true in fallback payload")
	}
	if _, ok := payload["messages_before"]; ok {
		t.Fatal("fallback payload must not include messages_before")
	}
	if _, ok := payload["messages_after"]; ok {
		t.Fatal("fallback payload must not include messages_after")
	}
	if _, ok := payload["error"].(string); !ok {
		t.Fatal("fallback payload must include error message")
	}
}

func TestGetLogContextFieldsFiltering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/messages/test-index/target":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]any{
					"fields": map[string]any{
						"_id":       "target",
						"timestamp": "2024-01-01T00:00:00.000Z",
						"source":    "svc-target",
						"message":   "target message",
						"level":     "ERROR",
						"facility":  "kern",
					},
				},
				"index": "test-index",
			})
		case "/api/views/search/sync":
			call, err := parseContextSearchCall(r)
			if err != nil {
				t.Fatalf("failed to parse search call: %v", err)
			}
			switch call.Order {
			case "DESC":
				writeViewsSearchResponse(w, 1, []testLogMessage{
					{
						ID: "before-1", Timestamp: "2023-12-31T23:59:59.000Z", Source: "svc", Message: "before1", Index: "idx",
						Extra: map[string]any{"level": "INFO", "facility": "auth"},
					},
				})
			case "ASC":
				writeViewsSearchResponse(w, 1, []testLogMessage{
					{
						ID: "after-1", Timestamp: "2024-01-01T00:00:01.000Z", Source: "svc", Message: "after1", Index: "idx",
						Extra: map[string]any{"level": "WARN", "facility": "web"},
					},
				})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := graylog.NewClient(server.URL, "token", "token", false, 2*time.Second)
	handler := getLogContextHandler(client)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"message_id": "target",
		"index":      "test-index",
		"before":     float64(1),
		"after":      float64(1),
		"fields":     "timestamp,source,message,level",
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	payload := decodeToolResultJSON(t, result)

	// Check target message: level should be present, facility should be filtered out
	targetMsg := payload["target_message"].(map[string]any)["message"].(map[string]any)
	if targetMsg["level"] != "ERROR" {
		t.Fatalf("expected target level=ERROR, got %v", targetMsg["level"])
	}
	if _, exists := targetMsg["facility"]; exists {
		t.Fatal("target facility should be filtered out when fields param is set")
	}

	// Check before messages
	beforeMsgs := payload["messages_before"].([]any)
	if len(beforeMsgs) != 1 {
		t.Fatalf("expected 1 before message, got %d", len(beforeMsgs))
	}
	beforeMsg := beforeMsgs[0].(map[string]any)["message"].(map[string]any)
	if beforeMsg["level"] != "INFO" {
		t.Fatalf("expected before level=INFO, got %v", beforeMsg["level"])
	}
	if _, exists := beforeMsg["facility"]; exists {
		t.Fatal("before facility should be filtered out")
	}

	// Check after messages
	afterMsgs := payload["messages_after"].([]any)
	if len(afterMsgs) != 1 {
		t.Fatalf("expected 1 after message, got %d", len(afterMsgs))
	}
	afterMsg := afterMsgs[0].(map[string]any)["message"].(map[string]any)
	if afterMsg["level"] != "WARN" {
		t.Fatalf("expected after level=WARN, got %v", afterMsg["level"])
	}
	if _, exists := afterMsg["facility"]; exists {
		t.Fatal("after facility should be filtered out")
	}
}

func parseContextSearchCall(r *http.Request) (contextSearchCall, error) {
	var req struct {
		Queries []struct {
			SearchTypes []struct {
				Limit int `json:"limit"`
				Sort  []struct {
					Order string `json:"order"`
				} `json:"sort"`
			} `json:"search_types"`
		} `json:"queries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return contextSearchCall{}, err
	}

	call := contextSearchCall{}
	if len(req.Queries) == 0 || len(req.Queries[0].SearchTypes) == 0 {
		return call, nil
	}

	st := req.Queries[0].SearchTypes[0]
	call.Limit = st.Limit
	if len(st.Sort) > 0 {
		call.Order = st.Sort[0].Order
	}
	return call, nil
}

func extractContextMessageIDs(t *testing.T, payload map[string]any, key string) []string {
	t.Helper()

	rawMessages, ok := payload[key].([]any)
	if !ok {
		t.Fatalf("%s has unexpected type %T", key, payload[key])
	}

	ids := make([]string, 0, len(rawMessages))
	for _, raw := range rawMessages {
		wrapper, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("%s wrapper has unexpected type %T", key, raw)
		}
		rawMessage, ok := wrapper["message"].(map[string]any)
		if !ok {
			t.Fatalf("%s.message has unexpected type %T", key, wrapper["message"])
		}
		id, ok := rawMessage["_id"].(string)
		if !ok {
			t.Fatalf("%s.message._id has unexpected type %T", key, rawMessage["_id"])
		}
		ids = append(ids, id)
	}
	return ids
}
