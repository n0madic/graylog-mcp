package tools

import (
	"context"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/n0madic/graylog-mcp/graylog"
)

func listFieldsTool() mcp.Tool {
	return mcp.NewTool("list_fields",
		mcp.WithDescription("List available log field names in Graylog. Useful for discovering queryable fields."),
		mcp.WithString("name_filter",
			mcp.Description("Optional substring filter for field names (case-insensitive)"),
		),
	)
}

func listFieldsHandler(getClient ClientFunc) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		nameFilter := strings.ToLower(getStringParam(args, "name_filter"))

		c := getClient(ctx)
		if c == nil {
			return toolError("no Graylog credentials: Authorization header required"), nil
		}
		resp, err := c.GetFields(ctx)
		if err != nil {
			if apiErr, ok := err.(*graylog.APIError); ok {
				return toolError(apiErr.Error()), nil
			}
			return toolError("Failed to get fields: " + err.Error()), nil
		}

		var fields []string
		for name := range resp {
			if nameFilter != "" && !strings.Contains(strings.ToLower(name), nameFilter) {
				continue
			}
			fields = append(fields, name)
		}

		sort.Strings(fields)

		return toolSuccess(map[string]any{
			"fields": fields,
			"total":  len(fields),
		}), nil
	}
}
