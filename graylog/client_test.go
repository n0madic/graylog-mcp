package graylog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSearchSurfacesQueryErrors verifies that when Graylog returns HTTP 200
// with a populated errors array in the query result (e.g., query parse error,
// invalid sort field, stream permission issue), the Search method surfaces
// the Graylog error description instead of producing a generic
// "missing search type 'msgs'" message that hides the real cause.
func TestSearchSurfacesQueryErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"execution": map[string]any{
				"completed_exceptionally": true,
				"done":                    true,
				"cancelled":               false,
			},
			"results": map[string]any{
				"q1": map[string]any{
					"search_types": map[string]any{},
					"errors": []map[string]any{
						{
							"description":    "Unable to parse query: foo:bar::",
							"search_type_id": "msgs",
							"type":           "QUERY_ERROR",
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "user", "pass", false, 5*time.Second)
	_, err := c.Search(context.Background(), SearchParams{Query: "foo:bar::", Limit: 10})
	if err == nil {
		t.Fatal("expected error from Search when Graylog returns query errors, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Unable to parse query: foo:bar::") {
		t.Errorf("expected error to contain Graylog description, got: %q", msg)
	}
	if strings.Contains(msg, "missing search type") {
		t.Errorf("error should not fall through to generic missing-msgs message, got: %q", msg)
	}
}

// TestSearchEmptyResults verifies that an empty result set (msgs present with
// zero messages) succeeds and returns an empty response — this is NOT the case
// that triggers the "missing search type" error.
func TestSearchEmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": map[string]any{
				"q1": map[string]any{
					"search_types": map[string]any{
						"msgs": map[string]any{
							"total_results": 0,
							"messages":      []any{},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "user", "pass", false, 5*time.Second)
	resp, err := c.Search(context.Background(), SearchParams{Query: "*", Limit: 10})
	if err != nil {
		t.Fatalf("empty results should not produce error, got: %v", err)
	}
	if resp.TotalResults != 0 {
		t.Errorf("expected total_results=0, got %d", resp.TotalResults)
	}
	if len(resp.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(resp.Messages))
	}
}
