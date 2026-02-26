package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/n0madic/graylog-mcp/graylog"
)

func TestSearchLogsHandlerRejectsInvalidNumericParams(t *testing.T) {
	client := graylog.NewClient("https://graylog.example.com", "token", "token", false, 2*time.Second)
	handler := searchLogsHandler(func(_ context.Context) *graylog.Client { return client })

	tests := []struct {
		name string
		args map[string]any
	}{
		{name: "negative offset", args: map[string]any{"query": "*", "offset": float64(-1)}},
		{name: "fractional offset", args: map[string]any{"query": "*", "offset": 1.5}},
		{name: "negative range", args: map[string]any{"query": "*", "range": float64(-5)}},
		{name: "fractional range", args: map[string]any{"query": "*", "range": 10.25}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args

			result, err := handler(context.Background(), req)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if !result.IsError {
				t.Fatalf("expected IsError=true for invalid args %#v", tt.args)
			}
		})
	}
}

func TestExecuteSearchDedupHonorsLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/views/search/sync" {
			http.NotFound(w, r)
			return
		}
		writeViewsSearchResponse(w, 20, []testLogMessage{
			{ID: "id-1", Timestamp: "2024-01-01T00:00:00.000Z", Source: "svc-a", Message: "duplicate-a", Index: "idx"},
			{ID: "id-2", Timestamp: "2024-01-01T00:00:01.000Z", Source: "svc-a", Message: "duplicate-a", Index: "idx"},
			{ID: "id-3", Timestamp: "2024-01-01T00:00:02.000Z", Source: "svc-b", Message: "duplicate-b", Index: "idx"},
			{ID: "id-4", Timestamp: "2024-01-01T00:00:03.000Z", Source: "svc-b", Message: "duplicate-b", Index: "idx"},
			{ID: "id-5", Timestamp: "2024-01-01T00:00:04.000Z", Source: "svc-c", Message: "unique-1", Index: "idx"},
			{ID: "id-6", Timestamp: "2024-01-01T00:00:05.000Z", Source: "svc-d", Message: "unique-2", Index: "idx"},
			{ID: "id-7", Timestamp: "2024-01-01T00:00:06.000Z", Source: "svc-e", Message: "unique-3", Index: "idx"},
			{ID: "id-8", Timestamp: "2024-01-01T00:00:07.000Z", Source: "svc-f", Message: "unique-4", Index: "idx"},
		})
	}))
	defer server.Close()

	client := graylog.NewClient(server.URL, "token", "token", false, 2*time.Second)
	result, err := executeSearch(context.Background(), client, graylog.SearchParams{
		Query: "*",
		Limit: 3,
	}, true, 50000)
	if err != nil {
		t.Fatalf("executeSearch returned error: %v", err)
	}

	payload := decodeToolResultJSON(t, result)
	deduplicated, ok := payload["deduplicated"].([]any)
	if !ok {
		t.Fatalf("deduplicated has unexpected type %T", payload["deduplicated"])
	}
	if len(deduplicated) != 3 {
		t.Fatalf("expected 3 deduplicated rows, got %d", len(deduplicated))
	}

	uniqueCount, ok := payload["unique_in_batch"].(float64)
	if !ok {
		t.Fatalf("unique_in_batch has unexpected type %T", payload["unique_in_batch"])
	}
	if uniqueCount != 6 {
		t.Fatalf("expected unique_in_batch=6, got %v", uniqueCount)
	}

	hasMore, ok := payload["has_more"].(bool)
	if !ok {
		t.Fatalf("has_more has unexpected type %T", payload["has_more"])
	}
	if !hasMore {
		t.Fatalf("expected has_more=true after dedup limit cap")
	}

	if _, exists := payload["query_time_ms"]; exists {
		t.Fatal("query_time_ms should not be present in search_logs response")
	}
}

func TestExecuteSearchDedupWithOffset(t *testing.T) {
	// 8 messages, 6 unique after dedup. With offset=2, limit=2 we should get unique[2] and unique[3].
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeViewsSearchResponse(w, 20, []testLogMessage{
			{ID: "id-1", Timestamp: "2024-01-01T00:00:00.000Z", Source: "svc-a", Message: "dup-a", Index: "idx"},
			{ID: "id-2", Timestamp: "2024-01-01T00:00:01.000Z", Source: "svc-a", Message: "dup-a", Index: "idx"},
			{ID: "id-3", Timestamp: "2024-01-01T00:00:02.000Z", Source: "svc-b", Message: "dup-b", Index: "idx"},
			{ID: "id-4", Timestamp: "2024-01-01T00:00:03.000Z", Source: "svc-b", Message: "dup-b", Index: "idx"},
			{ID: "id-5", Timestamp: "2024-01-01T00:00:04.000Z", Source: "svc-c", Message: "unique-1", Index: "idx"},
			{ID: "id-6", Timestamp: "2024-01-01T00:00:05.000Z", Source: "svc-d", Message: "unique-2", Index: "idx"},
			{ID: "id-7", Timestamp: "2024-01-01T00:00:06.000Z", Source: "svc-e", Message: "unique-3", Index: "idx"},
			{ID: "id-8", Timestamp: "2024-01-01T00:00:07.000Z", Source: "svc-f", Message: "unique-4", Index: "idx"},
		})
	}))
	defer server.Close()

	client := graylog.NewClient(server.URL, "token", "token", false, 2*time.Second)
	result, err := executeSearch(context.Background(), client, graylog.SearchParams{
		Query:  "*",
		Limit:  2,
		Offset: 2,
	}, true, 50000)
	if err != nil {
		t.Fatalf("executeSearch returned error: %v", err)
	}

	payload := decodeToolResultJSON(t, result)
	deduplicated, ok := payload["deduplicated"].([]any)
	if !ok {
		t.Fatalf("deduplicated has unexpected type %T", payload["deduplicated"])
	}
	if len(deduplicated) != 2 {
		t.Fatalf("expected 2 deduplicated rows, got %d", len(deduplicated))
	}

	// Verify offset=2 means we skip the first 2 unique groups (dup-a, dup-b)
	// and get unique-1, unique-2
	first := deduplicated[0].(map[string]any)["message"].(map[string]any)
	if first["message"] != "unique-1" {
		t.Fatalf("expected first result to be unique-1 after offset, got %v", first["message"])
	}
	second := deduplicated[1].(map[string]any)["message"].(map[string]any)
	if second["message"] != "unique-2" {
		t.Fatalf("expected second result to be unique-2 after offset, got %v", second["message"])
	}

	if uniqueCount := payload["unique_in_batch"].(float64); uniqueCount != 6 {
		t.Fatalf("expected unique_in_batch=6, got %v", uniqueCount)
	}
	if offset := payload["offset"].(float64); offset != 2 {
		t.Fatalf("expected offset=2 in response, got %v", offset)
	}
	if !payload["has_more"].(bool) {
		t.Fatal("expected has_more=true (2 more unique results remain)")
	}
}

func TestExecuteSearchDedupRespectsFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeViewsSearchResponse(w, 1, []testLogMessage{
			{
				ID: "id-1", Timestamp: "2024-01-01T00:00:00.000Z", Source: "svc-a", Message: "hello", Index: "idx",
				Extra: map[string]any{"level": "ERROR", "facility": "kern", "http_method": "GET"},
			},
		})
	}))
	defer server.Close()

	client := graylog.NewClient(server.URL, "token", "token", false, 2*time.Second)
	result, err := executeSearch(context.Background(), client, graylog.SearchParams{
		Query:  "*",
		Limit:  10,
		Fields: "timestamp,source,message,level",
	}, true, 50000)
	if err != nil {
		t.Fatalf("executeSearch returned error: %v", err)
	}

	payload := decodeToolResultJSON(t, result)
	deduplicated := payload["deduplicated"].([]any)
	if len(deduplicated) != 1 {
		t.Fatalf("expected 1 result, got %d", len(deduplicated))
	}

	msg := deduplicated[0].(map[string]any)["message"].(map[string]any)
	// "level" should be present (in fieldList)
	if msg["level"] != "ERROR" {
		t.Fatalf("expected level=ERROR, got %v", msg["level"])
	}
	// "facility" and "http_method" should be filtered out
	if _, exists := msg["facility"]; exists {
		t.Fatal("facility should be filtered out when fields param is set")
	}
	if _, exists := msg["http_method"]; exists {
		t.Fatal("http_method should be filtered out when fields param is set")
	}
}

func TestExecuteSearchOmitsQueryTimeInNonDedupMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/views/search/sync" {
			http.NotFound(w, r)
			return
		}
		writeViewsSearchResponse(w, 1, []testLogMessage{
			{ID: "id-1", Timestamp: "2024-01-01T00:00:00.000Z", Source: "svc-a", Message: "hello", Index: "idx"},
		})
	}))
	defer server.Close()

	client := graylog.NewClient(server.URL, "token", "token", false, 2*time.Second)
	result, err := executeSearch(context.Background(), client, graylog.SearchParams{
		Query: "*",
		Limit: 10,
	}, false, 50000)
	if err != nil {
		t.Fatalf("executeSearch returned error: %v", err)
	}

	payload := decodeToolResultJSON(t, result)
	if _, exists := payload["query_time_ms"]; exists {
		t.Fatal("query_time_ms should not be present in non-dedup search_logs response")
	}
}
