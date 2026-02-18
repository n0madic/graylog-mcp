package tools

import (
	"github.com/mark3labs/mcp-go/server"
	"github.com/n0madic/graylog-mcp/graylog"
)

func RegisterAll(s *server.MCPServer, client *graylog.Client) {
	s.AddTool(searchLogsTool(), searchLogsHandler(client))
	s.AddTool(listStreamsTool(), listStreamsHandler(client))
	s.AddTool(listFieldsTool(), listFieldsHandler(client))
	s.AddTool(getLogContextTool(), getLogContextHandler(client))
	s.AddTool(aggregateLogsTool(), aggregateLogsHandler(client))
}
