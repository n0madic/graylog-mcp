package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/n0madic/graylog-mcp/graylog"
)

const (
	contextResultMaxSize        = 50000
	contextOverfetchMultiplier  = 3
	contextMaxFetchLimitPerSide = 1501
)

func getLogContextTool() mcp.Tool {
	return mcp.NewTool("get_log_context",
		mcp.WithDescription("Get surrounding log messages around a specific message. Useful for understanding the context of an event."),
		mcp.WithString("message_id",
			mcp.Required(),
			mcp.Description("The _id of the target message"),
		),
		mcp.WithString("index",
			mcp.Required(),
			mcp.Description("The Elasticsearch index of the target message"),
		),
		mcp.WithNumber("before",
			mcp.Description("Number of messages to fetch before the target (default: 5)"),
		),
		mcp.WithNumber("after",
			mcp.Description("Number of messages to fetch after the target (default: 5)"),
		),
		mcp.WithString("fields",
			mcp.Description("Comma-separated list of fields to return"),
		),
		mcp.WithString("stream_id",
			mcp.Description("Optional stream ID to restrict context search to a specific stream"),
		),
	)
}

func getLogContextHandler(getClient ClientFunc) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c := getClient(ctx)
		if c == nil {
			return toolError("no Graylog credentials: Authorization header required"), nil
		}

		args := request.GetArguments()

		messageID := getStringParam(args, "message_id")
		if messageID == "" {
			return toolError("'message_id' parameter is required"), nil
		}

		index := getStringParam(args, "index")
		if index == "" {
			return toolError("'index' parameter is required"), nil
		}

		before, err := getStrictNonNegativeIntParam(args, "before", 5)
		if err != nil {
			return toolError(err.Error()), nil
		}
		if before > 500 {
			before = 500
		}
		after, err := getStrictNonNegativeIntParam(args, "after", 5)
		if err != nil {
			return toolError(err.Error()), nil
		}
		if after > 500 {
			after = 500
		}
		fields := getStringParam(args, "fields")
		streamID := getStringParam(args, "stream_id")

		var streamIDs []string
		if streamID != "" {
			streamIDs = []string{streamID}
		}

		// Fetch the target message
		target, err := c.GetMessage(ctx, index, messageID)
		if err != nil {
			if apiErr, ok := err.(*graylog.APIError); ok {
				return toolError(apiErr.Error()), nil
			}
			return toolError("Failed to get message: " + err.Error()), nil
		}

		timestamp := target.Message.Timestamp

		result := map[string]any{
			"target_message": target,
		}

		beforeLimit := min(before*contextOverfetchMultiplier+1, contextMaxFetchLimitPerSide)
		afterLimit := min(after*contextOverfetchMultiplier+1, contextMaxFetchLimitPerSide)

		// Search for messages before
		messagesBefore := make([]graylog.MessageWrapper, 0)
		if before > 0 {
			beforeParams := graylog.SearchParams{
				Query:     "*",
				From:      "1970-01-01T00:00:00.000Z",
				To:        timestamp,
				Limit:     beforeLimit, // +1 to account for the target message itself
				Sort:      "timestamp:desc",
				Fields:    fields,
				StreamIDs: streamIDs,
			}
			beforeResp, err := c.Search(ctx, beforeParams)
			if err != nil {
				result["before_error"] = err.Error()
			} else {
				messagesBefore = filterOutContextMessageID(beforeResp.Messages, messageID)
			}
		}
		// Reverse to chronological order
		for i, j := 0, len(messagesBefore)-1; i < j; i, j = i+1, j-1 {
			messagesBefore[i], messagesBefore[j] = messagesBefore[j], messagesBefore[i]
		}
		messagesBefore = deduplicateContextMessagesByID(messagesBefore)
		if len(messagesBefore) > before {
			messagesBefore = messagesBefore[:before]
		}

		// Search for messages after
		messagesAfter := make([]graylog.MessageWrapper, 0)
		if after > 0 {
			afterParams := graylog.SearchParams{
				Query:     "*",
				From:      timestamp,
				To:        "2099-12-31T23:59:59.999Z",
				Limit:     afterLimit,
				Sort:      "timestamp:asc",
				Fields:    fields,
				StreamIDs: streamIDs,
			}
			afterResp, err := c.Search(ctx, afterParams)
			if err != nil {
				result["after_error"] = err.Error()
			} else {
				messagesAfter = filterOutContextMessageID(afterResp.Messages, messageID)
			}
		}

		messagesAfter = deduplicateContextMessagesByID(messagesAfter)
		messagesAfter = removeContextOverlapByID(messagesAfter, messagesBefore)
		if len(messagesAfter) > after {
			messagesAfter = messagesAfter[:after]
		}

		// Filter Extra fields if user requested specific fields
		if fields != "" {
			fieldSet := make(map[string]bool)
			for _, f := range strings.Split(fields, ",") {
				fieldSet[strings.TrimSpace(f)] = true
			}
			for i := range messagesBefore {
				filterMessageExtraFields(messagesBefore[i].Message.Extra, fieldSet)
			}
			for i := range messagesAfter {
				filterMessageExtraFields(messagesAfter[i].Message.Extra, fieldSet)
			}
			if target != nil {
				filterMessageExtraFields(target.Message.Extra, fieldSet)
			}
		}

		result["messages_before"] = messagesBefore
		result["messages_after"] = messagesAfter
		result["context_incomplete"] = len(messagesBefore) < before || len(messagesAfter) < after

		return fitContextResult(result, contextResultMaxSize)
	}
}

func fitContextResult(result map[string]any, maxSize int) (*mcp.CallToolResult, error) {
	return fitResult(result, maxSize, resultAdapter{
		truncateMsgs: func(maxLen int) {
			truncateContextMessages(result, maxLen)
		},
		reduceMsgs: func() bool {
			before := contextMessageCount(result, "messages_before")
			after := contextMessageCount(result, "messages_after")
			if before+after <= 2 {
				return false
			}
			newBefore := before / 2
			if newBefore < 1 && before > 0 {
				newBefore = 1
			}
			newAfter := after / 2
			if newAfter < 1 && after > 0 {
				newAfter = 1
			}
			reduceContextMessages(result, "messages_before", newBefore)
			reduceContextMessages(result, "messages_after", newAfter)
			result["context_incomplete"] = true
			return true
		},
		lastResort: func() map[string]any {
			targetID, targetTimestamp, targetIndex := contextTargetMetadata(result["target_message"])
			metadata := map[string]any{
				"target_message_id":  targetID,
				"target_timestamp":   targetTimestamp,
				"target_index":       targetIndex,
				"context_incomplete": result["context_incomplete"],
				"has_more":           true,
				"response_truncated": true,
				"error":              "Context response too large even after truncation. Reduce 'before'/'after' or use 'fields' to limit payload size.",
			}
			if v, ok := result["before_error"]; ok {
				metadata["before_error"] = v
			}
			if v, ok := result["after_error"]; ok {
				metadata["after_error"] = v
			}
			return metadata
		},
	})
}

func filterOutContextMessageID(messages []graylog.MessageWrapper, messageID string) []graylog.MessageWrapper {
	filtered := make([]graylog.MessageWrapper, 0, len(messages))
	for _, mw := range messages {
		if mw.Message.ID == messageID {
			continue
		}
		filtered = append(filtered, mw)
	}
	return filtered
}

func deduplicateContextMessagesByID(messages []graylog.MessageWrapper) []graylog.MessageWrapper {
	seen := make(map[string]struct{}, len(messages))
	deduplicated := make([]graylog.MessageWrapper, 0, len(messages))
	for _, mw := range messages {
		if mw.Message.ID == "" {
			deduplicated = append(deduplicated, mw)
			continue
		}
		if _, ok := seen[mw.Message.ID]; ok {
			continue
		}
		seen[mw.Message.ID] = struct{}{}
		deduplicated = append(deduplicated, mw)
	}
	return deduplicated
}

func removeContextOverlapByID(after, before []graylog.MessageWrapper) []graylog.MessageWrapper {
	if len(after) == 0 || len(before) == 0 {
		return after
	}

	beforeIDs := make(map[string]struct{}, len(before))
	for _, mw := range before {
		if mw.Message.ID == "" {
			continue
		}
		beforeIDs[mw.Message.ID] = struct{}{}
	}
	if len(beforeIDs) == 0 {
		return after
	}

	filtered := make([]graylog.MessageWrapper, 0, len(after))
	for _, mw := range after {
		if mw.Message.ID == "" {
			filtered = append(filtered, mw)
			continue
		}
		if _, overlap := beforeIDs[mw.Message.ID]; overlap {
			continue
		}
		filtered = append(filtered, mw)
	}
	return filtered
}

func contextTargetMetadata(target any) (id, timestamp, index string) {
	switch t := target.(type) {
	case *graylog.MessageWrapper:
		if t != nil {
			return t.Message.ID, t.Message.Timestamp, t.Index
		}
	case graylog.MessageWrapper:
		return t.Message.ID, t.Message.Timestamp, t.Index
	}
	return "", "", ""
}

func truncateContextMessages(result map[string]any, maxLen int) {
	// Truncate target message
	if target, ok := result["target_message"].(*graylog.MessageWrapper); ok && target != nil {
		target.Message.Message = truncateString(target.Message.Message, maxLen)
	}

	// Truncate before messages
	if messages, ok := result["messages_before"].([]graylog.MessageWrapper); ok {
		for i := range messages {
			messages[i].Message.Message = truncateString(messages[i].Message.Message, maxLen)
		}
	}

	// Truncate after messages
	if messages, ok := result["messages_after"].([]graylog.MessageWrapper); ok {
		for i := range messages {
			messages[i].Message.Message = truncateString(messages[i].Message.Message, maxLen)
		}
	}
}

func contextMessageCount(result map[string]any, key string) int {
	if messages, ok := result[key].([]graylog.MessageWrapper); ok {
		return len(messages)
	}
	return 0
}

func reduceContextMessages(result map[string]any, key string, count int) {
	if messages, ok := result[key].([]graylog.MessageWrapper); ok {
		if count < len(messages) {
			result[key] = messages[:count]
		}
	}
}
