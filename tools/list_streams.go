package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/n0madic/graylog-mcp/graylog"
)

func listStreamsTool() mcp.Tool {
	return mcp.NewTool("list_streams",
		mcp.WithDescription("List available Graylog streams. Streams organize log messages into categories."),
		mcp.WithString("title_filter",
			mcp.Description("Optional substring filter for stream titles (case-insensitive)"),
		),
	)
}

func listStreamsHandler(getClient ClientFunc) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		titleFilter := strings.ToLower(getStringParam(args, "title_filter"))

		c := getClient(ctx)
		if c == nil {
			return toolError("no Graylog credentials: Authorization header required"), nil
		}
		resp, err := c.GetStreams(ctx)
		if err != nil {
			if apiErr, ok := err.(*graylog.APIError); ok {
				return toolError(apiErr.Error()), nil
			}
			return toolError("Failed to get streams: " + err.Error()), nil
		}

		type streamOutput struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			IndexSetID  string `json:"index_set_id"`
		}

		var streams []streamOutput
		for _, s := range resp.Streams {
			if s.Disabled {
				continue
			}
			if titleFilter != "" && !strings.Contains(strings.ToLower(s.Title), titleFilter) {
				continue
			}
			streams = append(streams, streamOutput{
				ID:          s.ID,
				Title:       s.Title,
				Description: s.Description,
				IndexSetID:  s.IndexSetID,
			})
		}

		return toolSuccess(map[string]any{
			"streams": streams,
			"total":   len(streams),
		}), nil
	}
}
