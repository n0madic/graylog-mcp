package tools

import (
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

type resultAdapter struct {
	truncateMsgs func(maxLen int)      // phase 1 — truncate message content
	reduceMsgs   func() bool           // phase 2 — reduce message count by ~half, returns false when can't reduce further
	lastResort   func() map[string]any // optional: return metadata-only fallback
}

func fitResult(result map[string]any, maxSize int, adapter resultAdapter) (*mcp.CallToolResult, error) {
	if maxSize <= 0 {
		return toolSuccess(result), nil
	}

	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return toolError("failed to marshal response: " + err.Error()), nil
	}

	if len(jsonBytes) <= maxSize {
		return toolSuccessJSON(jsonBytes), nil
	}

	// Phase 1: Progressive message truncation
	for _, truncLen := range []int{500, 200, 100, 50} {
		adapter.truncateMsgs(truncLen)
		result["response_truncated"] = true
		jsonBytes, err = json.Marshal(result)
		if err != nil {
			return toolError("failed to marshal response: " + err.Error()), nil
		}
		if len(jsonBytes) <= maxSize {
			return toolSuccessJSON(jsonBytes), nil
		}
	}

	// Phase 2: Reduce message count (bounded to prevent infinite loops)
	for i := 0; i < 20; i++ {
		if !adapter.reduceMsgs() {
			break
		}
		result["response_truncated"] = true
		jsonBytes, err = json.Marshal(result)
		if err != nil {
			return toolError("failed to marshal response: " + err.Error()), nil
		}
		if len(jsonBytes) <= maxSize {
			return toolSuccessJSON(jsonBytes), nil
		}
	}

	// Last resort
	if adapter.lastResort != nil {
		metadata := adapter.lastResort()
		jsonBytes, err = json.Marshal(metadata)
		if err != nil {
			return toolError("failed to marshal response: " + err.Error()), nil
		}
		return toolSuccessJSON(jsonBytes), nil
	}

	// Defensive: ensure response_truncated is set even if all reduction phases
	// failed to bring the response below maxSize (e.g. single oversized message).
	result["response_truncated"] = true
	jsonBytes, err = json.Marshal(result)
	if err != nil {
		return toolError("failed to marshal response: " + err.Error()), nil
	}
	return toolSuccessJSON(jsonBytes), nil
}
