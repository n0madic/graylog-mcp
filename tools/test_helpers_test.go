package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

type testLogMessage struct {
	ID        string
	Timestamp string
	Source    string
	Message   string
	Index     string
	Extra     map[string]any
}

func decodeToolResultJSON(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()

	if result == nil {
		t.Fatal("tool result is nil")
	}
	if len(result.Content) == 0 {
		t.Fatal("tool result has no content")
	}

	var text string
	switch content := result.Content[0].(type) {
	case mcp.TextContent:
		text = content.Text
	case *mcp.TextContent:
		text = content.Text
	default:
		t.Fatalf("unexpected tool content type %T", result.Content[0])
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("failed to unmarshal tool payload: %v", err)
	}
	return payload
}

func writeViewsSearchResponse(w http.ResponseWriter, totalResults int, messages []testLogMessage) {
	w.Header().Set("Content-Type", "application/json")

	serializedMessages := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		msgFields := map[string]any{
			"_id":       msg.ID,
			"timestamp": msg.Timestamp,
			"source":    msg.Source,
			"message":   msg.Message,
		}
		for k, v := range msg.Extra {
			msgFields[k] = v
		}
		serializedMessages = append(serializedMessages, map[string]any{
			"message": msgFields,
			"index":   msg.Index,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"results": map[string]any{
			"q1": map[string]any{
				"search_types": map[string]any{
					"msgs": map[string]any{
						"total_results": totalResults,
						"messages":      serializedMessages,
					},
				},
			},
		},
	})
}
