package tools

import (
	"github.com/mark3labs/mcp-go/server"
)

func RegisterAll(s *server.MCPServer, getClient ClientFunc) {
	s.AddTool(searchLogsTool(), searchLogsHandler(getClient))
	s.AddTool(listStreamsTool(), listStreamsHandler(getClient))
	s.AddTool(listFieldsTool(), listFieldsHandler(getClient))
	s.AddTool(getLogContextTool(), getLogContextHandler(getClient))
	s.AddTool(aggregateLogsTool(), aggregateLogsHandler(getClient))
}
