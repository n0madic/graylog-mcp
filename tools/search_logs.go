package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/n0madic/graylog-mcp/dedup"
	"github.com/n0madic/graylog-mcp/graylog"
)

func searchLogsTool() mcp.Tool {
	return mcp.NewTool("search_logs",
		mcp.WithDescription("Search Graylog logs globally using Lucene query syntax. Returns matching log messages with metadata."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Lucene query string (e.g. 'level:ERROR AND service:auth')"),
		),
		mcp.WithString("stream_id",
			mcp.Description("Graylog stream ID to search within"),
		),
		mcp.WithNumber("range",
			mcp.Description("Time range in seconds for relative search (default: 300). Ignored if from/to are set."),
		),
		mcp.WithString("from",
			mcp.Description("Start time in ISO8601 format (e.g. '2024-01-15T10:00:00.000Z'). Must be used with 'to'."),
		),
		mcp.WithString("to",
			mcp.Description("End time in ISO8601 format. Must be used with 'from'."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of messages to return (default: 50, max: 10000)"),
		),
		mcp.WithNumber("offset",
			mcp.Description("Number of messages to skip for pagination (default: 0)"),
		),
		mcp.WithString("fields",
			mcp.Description("Comma-separated list of fields to return (e.g. 'timestamp,source,message,level')"),
		),
		mcp.WithString("sort",
			mcp.Description("Sort order as 'field:asc' or 'field:desc' (e.g. 'timestamp:desc')"),
		),
		mcp.WithBoolean("deduplicate",
			mcp.Description("If true, deduplicate similar messages and show count"),
		),
		mcp.WithNumber("truncate_message",
			mcp.Description("Truncate message field to N characters (0 = no truncation). Useful to reduce output size when messages contain large stack traces."),
		),
		mcp.WithNumber("max_result_size",
			mcp.Description("Maximum size of the response in bytes (default: 50000, 0 = no limit). Response will be automatically truncated to fit within this limit."),
		),
	)
}

func searchLogsHandler(getClient ClientFunc) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

		query := getStringParam(args, "query")
		if query == "" {
			return toolError("'query' parameter is required"), nil
		}

		from := getStringParam(args, "from")
		to := getStringParam(args, "to")
		if (from == "") != (to == "") {
			return toolError("'from' and 'to' must be used together"), nil
		}

		limit, err := getStrictNonNegativeIntParam(args, "limit", 50)
		if err != nil {
			return toolError(err.Error()), nil
		}
		if limit > 10000 {
			limit = 10000
		}
		if limit < 1 {
			limit = 50
		}

		params := graylog.SearchParams{
			Query:  query,
			From:   from,
			To:     to,
			Limit:  limit,
			Fields: getStringParam(args, "fields"),
			Sort:   getStringParam(args, "sort"),
		}

		if streamID := getStringParam(args, "stream_id"); streamID != "" {
			params.StreamIDs = []string{streamID}
		}

		rangeVal, err := getStrictNonNegativeIntParam(args, "range", 0)
		if err != nil {
			return toolError(err.Error()), nil
		}
		params.Range = rangeVal

		offset, err := getStrictNonNegativeIntParam(args, "offset", 0)
		if err != nil {
			return toolError(err.Error()), nil
		}
		params.Offset = offset

		truncateMessage, err := getStrictNonNegativeIntParam(args, "truncate_message", 0)
		if err != nil {
			return toolError(err.Error()), nil
		}
		params.TruncateMessage = truncateMessage

		maxResultSize, err := getStrictNonNegativeIntParam(args, "max_result_size", 50000)
		if err != nil {
			return toolError(err.Error()), nil
		}

		c := getClient(ctx)
		if c == nil {
			return toolError("no Graylog credentials: Authorization header required"), nil
		}
		return executeSearch(ctx, c, params, getBoolParam(args, "deduplicate"), maxResultSize)
	}
}

// dedupFetchMultiplier controls how many more messages to fetch from Graylog
// when deduplication is enabled, to increase the chance of getting enough
// unique results despite duplicate messages in the stream.
const dedupFetchMultiplier = 3

func executeSearch(ctx context.Context, client *graylog.Client, params graylog.SearchParams, deduplicate bool, maxResultSize int) (*mcp.CallToolResult, error) {
	requestedLimit := params.Limit
	originalOffset := params.Offset

	// When deduplicating, fetch from offset=0 so dedup works across the full range.
	// Offset is applied to the deduplicated results afterwards.
	if deduplicate {
		params.Offset = 0
		params.Limit = min((originalOffset+requestedLimit)*dedupFetchMultiplier, 10000)
	}

	resp, err := client.Search(ctx, params)
	if err != nil {
		if apiErr, ok := err.(*graylog.APIError); ok {
			return toolError(apiErr.Error()), nil
		}
		return toolError("Search failed: " + err.Error()), nil
	}

	hasMoreFromPagination := originalOffset+requestedLimit < resp.TotalResults

	var fieldList []string
	if params.Fields != "" {
		for _, f := range strings.Split(params.Fields, ",") {
			fieldList = append(fieldList, strings.TrimSpace(f))
		}
	}

	// Apply truncate_message to ALL messages before dedup or building result
	// (fixes bug: was previously skipped for dedup path)
	truncate := params.TruncateMessage
	if truncate > 0 {
		for i := range resp.Messages {
			resp.Messages[i].Message.Message = truncateString(resp.Messages[i].Message.Message, truncate)
		}
	}

	if deduplicate && len(resp.Messages) > 0 {
		// Always hash by all fields — fieldList is for output filtering only.
		dedupResults := dedup.Deduplicate(resp.Messages, nil)
		uniqueCount := len(dedupResults)

		// Cap message_ids before any fitting (including when max_result_size=0).
		dedup.CapMessageIDs(dedupResults, 5)

		// Apply user's original offset to deduplicated results
		if originalOffset > 0 {
			if originalOffset < len(dedupResults) {
				dedupResults = dedupResults[originalOffset:]
			} else {
				dedupResults = nil
			}
		}

		if len(dedupResults) > requestedLimit {
			dedupResults = dedupResults[:requestedLimit]
		}
		hasMore := hasMoreFromPagination || uniqueCount > originalOffset+len(dedupResults)

		if len(fieldList) > 0 {
			filterDedupResultFields(dedupResults, fieldList)
		}

		result := map[string]any{
			"deduplicated":      dedupResults,
			"total_raw_results": resp.TotalResults,
			"unique_in_batch":   uniqueCount,
			"limit":             requestedLimit,
			"offset":            originalOffset,
			"has_more":          hasMore,
		}
		return fitSearchResult(result, maxResultSize, true)
	}

	messages := make([]map[string]any, len(resp.Messages))
	for i, wrapper := range resp.Messages {
		messages[i] = map[string]any{
			"message": wrapper.Message.ToFilteredMap(fieldList),
			"index":   wrapper.Index,
		}
	}

	result := map[string]any{
		"messages":      messages,
		"total_results": resp.TotalResults,
		"limit":         params.Limit,
		"offset":        params.Offset,
		"has_more":      hasMoreFromPagination,
	}

	return fitSearchResult(result, maxResultSize, false)
}

func fitSearchResult(result map[string]any, maxSize int, isDedup bool) (*mcp.CallToolResult, error) {
	adapter := resultAdapter{
		truncateMsgs: func(maxLen int) {
			truncateMessagesInResult(result, maxLen, isDedup)
		},
		reduceMsgs: func() bool {
			count := searchMessageCount(result, isDedup)
			if count <= 1 {
				return false
			}
			newCount := count / 2
			if newCount < 1 {
				newCount = 1
			}
			reduceMessagesInResult(result, newCount, isDedup)
			result["has_more"] = true
			return true
		},
		lastResort: func() map[string]any {
			totalKey := "total_results"
			if isDedup {
				totalKey = "total_raw_results"
			}
			metadata := map[string]any{
				totalKey:             result[totalKey],
				"limit":              result["limit"],
				"offset":             result["offset"],
				"has_more":           true,
				"response_truncated": true,
				"error":              "Response too large even after truncation. Use 'fields' parameter to select specific fields or 'truncate_message' to limit message size.",
			}
			if isDedup {
				metadata["unique_in_batch"] = result["unique_in_batch"]
			}
			return metadata
		},
	}

	return fitResult(result, maxSize, adapter)
}

// filterDedupResultFields removes Extra fields not in fieldList from each DedupResult.
// Known struct fields (timestamp, source, message) are always kept; _id is omitted by MarshalJSON.
func filterDedupResultFields(results []dedup.DedupResult, fieldList []string) {
	fieldSet := make(map[string]bool, len(fieldList))
	for _, f := range fieldList {
		fieldSet[f] = true
	}
	for i := range results {
		for k := range results[i].Message.Extra {
			if !fieldSet[k] {
				delete(results[i].Message.Extra, k)
			}
		}
	}
}

func truncateMessagesInResult(result map[string]any, maxLen int, isDedup bool) {
	if isDedup {
		if dedupResults, ok := result["deduplicated"].([]dedup.DedupResult); ok {
			for i := range dedupResults {
				dedupResults[i].Message.Message = truncateString(dedupResults[i].Message.Message, maxLen)
			}
		}
	} else {
		if messages, ok := result["messages"].([]map[string]any); ok {
			for _, wrapper := range messages {
				if msgMap, ok := wrapper["message"].(map[string]any); ok {
					if msgStr, ok := msgMap["message"].(string); ok {
						msgMap["message"] = truncateString(msgStr, maxLen)
					}
				}
			}
		}
	}
}

func searchMessageCount(result map[string]any, isDedup bool) int {
	if isDedup {
		if dedupResults, ok := result["deduplicated"].([]dedup.DedupResult); ok {
			return len(dedupResults)
		}
	} else {
		if messages, ok := result["messages"].([]map[string]any); ok {
			return len(messages)
		}
	}
	return 0
}

func reduceMessagesInResult(result map[string]any, count int, isDedup bool) {
	if isDedup {
		if dedupResults, ok := result["deduplicated"].([]dedup.DedupResult); ok {
			if count < len(dedupResults) {
				result["deduplicated"] = dedupResults[:count]
			}
		}
	} else {
		if messages, ok := result["messages"].([]map[string]any); ok {
			if count < len(messages) {
				result["messages"] = messages[:count]
			}
		}
	}
}
