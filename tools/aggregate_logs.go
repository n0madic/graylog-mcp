package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/n0madic/graylog-mcp/graylog"
)

var validAggFunctions = map[string]bool{
	"count":        true,
	"avg":          true,
	"min":          true,
	"max":          true,
	"sum":          true,
	"stddev":       true,
	"variance":     true,
	"card":         true,
	"percentile":   true,
	"latest":       true,
	"sumofsquares": true,
}

func aggregateLogsTool() mcp.Tool {
	return mcp.NewTool("aggregate_logs",
		mcp.WithDescription("Aggregate Graylog logs using statistical functions (count, avg, min, max, percentile, etc.) with optional grouping. Uses Graylog Scripting API."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Lucene query string (e.g. 'level:ERROR AND service:auth')"),
		),
		mcp.WithString("metrics",
			mcp.Required(),
			mcp.Description("Comma-separated metrics: 'count', 'avg:field', 'min:field', 'max:field', 'sum:field', 'percentile:field:value', 'card:field', 'stddev:field', 'variance:field', 'latest:field'"),
		),
		mcp.WithString("group_by",
			mcp.Required(),
			mcp.Description("Comma-separated fields to group by (e.g. 'source', 'source,level')"),
		),
		mcp.WithNumber("group_limit",
			mcp.Description("Maximum number of groups per field (default: 10)"),
		),
		mcp.WithString("stream_id",
			mcp.Description("Graylog stream ID to search within"),
		),
		mcp.WithNumber("range",
			mcp.Description("Time range in seconds for relative search (default: 300). Ignored if from/to or timerange_keyword are set."),
		),
		mcp.WithString("from",
			mcp.Description("Start time in ISO8601 format (e.g. '2024-01-15T10:00:00.000Z'). Must be used with 'to'."),
		),
		mcp.WithString("to",
			mcp.Description("End time in ISO8601 format. Must be used with 'from'."),
		),
		mcp.WithString("timerange_keyword",
			mcp.Description("Natural language time range (e.g. 'last five minutes'). Mutually exclusive with from/to and range."),
		),
		mcp.WithString("sort",
			mcp.Description("Sort direction for the first metric: 'asc' or 'desc'"),
		),
		mcp.WithNumber("max_result_size",
			mcp.Description("Maximum size of the response in bytes (default: 50000, 0 = no limit). Response will be automatically truncated to fit within this limit."),
		),
	)
}

func aggregateLogsHandler(client *graylog.Client) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.Params.Arguments

		query := getStringParam(args, "query")
		if query == "" {
			return toolError("'query' parameter is required"), nil
		}

		metricsStr := getStringParam(args, "metrics")
		if metricsStr == "" {
			return toolError("'metrics' parameter is required"), nil
		}

		metrics, err := parseMetrics(metricsStr, getStringParam(args, "sort"))
		if err != nil {
			return toolError(err.Error()), nil
		}

		from := getStringParam(args, "from")
		to := getStringParam(args, "to")
		keyword := getStringParam(args, "timerange_keyword")

		if (from == "") != (to == "") {
			return toolError("'from' and 'to' must be used together"), nil
		}

		timeRange, err := buildScriptingTimeRange(from, to, keyword, getIntParam(args, "range", 0))
		if err != nil {
			return toolError(err.Error()), nil
		}

		groupByStr := getStringParam(args, "group_by")
		if groupByStr == "" {
			return toolError("'group_by' parameter is required"), nil
		}

		groupBy := parseGroupBy(groupByStr, getIntParam(args, "group_limit", 10))

		req := graylog.ScriptingAggregateRequest{
			Query:     query,
			TimeRange: timeRange,
			GroupBy:   groupBy,
			Metrics:   metrics,
		}

		if streamID := getStringParam(args, "stream_id"); streamID != "" {
			req.Streams = []string{streamID}
		}

		resp, err := client.Aggregate(ctx, req)
		if err != nil {
			if apiErr, ok := err.(*graylog.APIError); ok {
				return toolError(apiErr.Error()), nil
			}
			return toolError("Aggregate failed: " + err.Error()), nil
		}

		rows := tabularToRows(resp.Schema, resp.DataRows)

		result := map[string]any{
			"rows":       rows,
			"total_rows": len(rows),
			"metadata":   resp.Metadata,
		}

		maxResultSize := getIntParam(args, "max_result_size", 50000)
		return fitAggregateResult(result, maxResultSize)
	}
}

func parseMetrics(metricsStr, sort string) ([]graylog.ScriptingMetric, error) {
	parts := strings.Split(metricsStr, ",")
	metrics := make([]graylog.ScriptingMetric, 0, len(parts))

	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		segments := strings.SplitN(part, ":", 3)
		fn := strings.ToLower(strings.TrimSpace(segments[0]))

		if !validAggFunctions[fn] {
			return nil, fmt.Errorf("unknown aggregation function '%s'. Valid functions: count, avg, min, max, sum, stddev, variance, card, percentile, latest, sumofsquares", fn)
		}

		m := graylog.ScriptingMetric{Function: fn}

		if fn == "count" {
			// count does not require a field, but can optionally have one
			if len(segments) > 1 {
				m.Field = strings.TrimSpace(segments[1])
			}
		} else if fn == "percentile" {
			if len(segments) < 3 {
				return nil, fmt.Errorf("percentile requires format 'percentile:field:value' (e.g. 'percentile:took_ms:95')")
			}
			m.Field = strings.TrimSpace(segments[1])
			pctVal, err := strconv.ParseFloat(strings.TrimSpace(segments[2]), 64)
			if err != nil || pctVal <= 0 || pctVal > 100 {
				return nil, fmt.Errorf("percentile value must be a number between 0 and 100, got '%s'", segments[2])
			}
			m.Configuration = &graylog.ScriptingMetricConfig{Percentile: pctVal}
		} else {
			if len(segments) < 2 || strings.TrimSpace(segments[1]) == "" {
				return nil, fmt.Errorf("'%s' requires a field (e.g. '%s:field_name')", fn, fn)
			}
			m.Field = strings.TrimSpace(segments[1])
		}

		// Apply sort to the first metric only
		if i == 0 && sort != "" {
			sortLower := strings.ToLower(sort)
			if sortLower == "asc" || sortLower == "desc" {
				m.Sort = sortLower
			}
		}

		metrics = append(metrics, m)
	}

	if len(metrics) == 0 {
		return nil, fmt.Errorf("at least one metric is required")
	}

	return metrics, nil
}

func parseGroupBy(groupByStr string, limit int) []graylog.ScriptingGrouping {
	if groupByStr == "" {
		return nil
	}

	fields := strings.Split(groupByStr, ",")
	groups := make([]graylog.ScriptingGrouping, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			g := graylog.ScriptingGrouping{Field: f}
			if limit > 0 {
				g.Limit = limit
			}
			groups = append(groups, g)
		}
	}
	return groups
}

func buildScriptingTimeRange(from, to, keyword string, rangeSeconds int) (graylog.ScriptingTimeRange, error) {
	if keyword != "" {
		if from != "" || to != "" {
			return graylog.ScriptingTimeRange{}, fmt.Errorf("'timerange_keyword' cannot be used with 'from'/'to'")
		}
		return graylog.ScriptingTimeRange{Type: "keyword", Keyword: keyword}, nil
	}

	if from != "" && to != "" {
		return graylog.ScriptingTimeRange{Type: "absolute", From: from, To: to}, nil
	}

	if rangeSeconds <= 0 {
		rangeSeconds = 300
	}
	return graylog.ScriptingTimeRange{Type: "relative", Range: rangeSeconds}, nil
}

func tabularToRows(schema []graylog.ScriptingSchemaEntry, dataRows [][]any) []map[string]any {
	rows := make([]map[string]any, 0, len(dataRows))
	for _, dataRow := range dataRows {
		row := make(map[string]any, len(schema))
		for j, entry := range schema {
			if j < len(dataRow) {
				row[entry.Name] = dataRow[j]
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func fitAggregateResult(result map[string]any, maxSize int) (*mcp.CallToolResult, error) {
	adapter := resultAdapter{
		truncateMsgs: func(maxLen int) {
			// Aggregation rows don't have message bodies to truncate — no-op
		},
		reduceMsgs: func() bool {
			rows, ok := result["rows"].([]map[string]any)
			if !ok || len(rows) <= 1 {
				return false
			}
			newCount := len(rows) / 2
			if newCount < 1 {
				newCount = 1
			}
			result["rows"] = rows[:newCount]
			result["rows_truncated"] = true
			return true
		},
		lastResort: func() map[string]any {
			return map[string]any{
				"total_rows":         result["total_rows"],
				"metadata":           result["metadata"],
				"response_truncated": true,
				"error":              "Aggregation response too large even after truncation. Try reducing group_limit or using fewer group_by fields.",
			}
		},
	}

	return fitResult(result, maxSize, adapter)
}
