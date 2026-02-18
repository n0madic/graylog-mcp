package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
)

// truncateString truncates s to at most maxBytes bytes, ensuring the cut
// happens at a valid UTF-8 boundary. If truncation occurs, "...[truncated]"
// is appended (the total may exceed maxBytes by the suffix length).
func truncateString(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Walk backwards from maxBytes to find a valid UTF-8 boundary
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes] + "...[truncated]"
}

func toolSuccess(data any) *mcp.CallToolResult {
	b, err := json.Marshal(data)
	if err != nil {
		return toolError(fmt.Sprintf("failed to marshal response: %v", err))
	}
	return mcp.NewToolResultText(string(b))
}

func toolSuccessJSON(data []byte) *mcp.CallToolResult {
	return mcp.NewToolResultText(string(data))
}

func toolError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: msg,
			},
		},
	}
}

func getStringParam(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getIntParam(args map[string]any, key string, defaultVal int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			if math.IsNaN(n) || n > math.MaxInt32 || n < math.MinInt32 {
				return defaultVal
			}
			return int(n)
		case int:
			return n
		case json.Number:
			if i, err := n.Int64(); err == nil {
				if i > math.MaxInt32 || i < math.MinInt32 {
					return defaultVal
				}
				return int(i)
			}
		}
	}
	return defaultVal
}

func getBoolParam(args map[string]any, key string) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getBoolParamDefault(args map[string]any, key string, defaultVal bool) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

// filterMessageExtraFields removes Extra map entries not in fieldSet from a Message.
// Known struct fields (_id, timestamp, source, message) are unaffected.
func filterMessageExtraFields(extra map[string]any, fieldSet map[string]bool) {
	for k := range extra {
		if !fieldSet[k] {
			delete(extra, k)
		}
	}
}
