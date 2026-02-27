# Graylog MCP Server

An [MCP](https://modelcontextprotocol.io/) (Model Context Protocol) server that gives LLMs the ability to search and analyze logs from [Graylog](https://graylog.org/). Works with Claude Desktop, Claude Code, Cursor, and any MCP-compatible client.

Single binary, zero runtime dependencies. Written in Go.

## Features

- **Search logs** with Lucene query syntax, time ranges, pagination, and sorting
- **Aggregate logs** with statistical functions (count, avg, min, max, percentile, etc.) and grouping
- **Stream filtering** to scope searches to specific Graylog streams
- **Log deduplication** to collapse repeated messages and show counts
- **Log template extraction** to discover common patterns using ULP pattern mining
- **Context retrieval** to see messages surrounding a specific log entry
- **Field discovery** to explore available log fields
- **Stream listing** to browse available Graylog streams
- **Automatic response fitting** to keep results within LLM context limits

## Installation

### go install

Requires Go 1.23+.

```bash
go install github.com/n0madic/graylog-mcp@latest
```

The binary will be placed in `$GOPATH/bin` (or `$HOME/go/bin` by default).

### Build from source

```bash
git clone https://github.com/n0madic/graylog-mcp.git
cd graylog-mcp
go build -o graylog-mcp .
```

## Configuration

The server is configured via environment variables or CLI flags. CLI flags take precedence over environment variables.

| Environment variable | CLI flag | Required | Default | Description |
|---|---|---|---|---|
| `GRAYLOG_URL` | `--url` | stdio: yes; http: no | - | Graylog base URL (http mode: can be passed per-request via `X-Graylog-URL` header instead) |
| `GRAYLOG_USERNAME` | `--username` | If no token | - | Username for Basic Auth |
| `GRAYLOG_PASSWORD` | `--password` | If no token | - | Password for Basic Auth |
| `GRAYLOG_TOKEN` | `--token` | If no credentials | - | API access token |
| `GRAYLOG_TLS_SKIP_VERIFY` | `--tls-skip-verify` | No | `false` | Skip TLS certificate verification |
| `GRAYLOG_TIMEOUT` | `--timeout` | No | `30s` | HTTP request timeout |
| `GRAYLOG_MCP_TRANSPORT` | `--transport` | No | `stdio` | Transport: `stdio` or `http` |
| `GRAYLOG_MCP_HTTP_BIND` | `--bind` | No | `0.0.0.0:8090` | HTTP listen address (http transport only) |

### Authentication

Two authentication methods are supported (at least one is required):

1. **Username & password** - standard Graylog credentials via Basic Auth
2. **API access token** - a Graylog access token (uses Basic Auth with `your_token:token` convention)

If both are provided, the token takes precedence.

## Transport modes

### stdio (default)

The binary is spawned as a subprocess by the MCP client. Credentials are configured via environment variables at startup. This is the mode used by Claude Desktop, Claude Code, and Cursor.

### http (Streamable HTTP)

The server listens for HTTP connections using the [MCP Streamable HTTP transport](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#streamable-http) (spec 2025-03-26). Designed for containerized deployments and tools like n8n that connect to MCP servers over the network.

In http mode, `GRAYLOG_URL` is optional on the server — it can be passed per-request via the `X-Graylog-URL` HTTP header. Similarly, credentials can be forwarded per-request via the `Authorization` header. This allows a single server instance to serve multiple pipelines, each with its own Graylog target and credentials. The MCP server only ever returns tool results to the LLM — credentials are never exposed.

## Usage with Claude Desktop

Add the server to your Claude Desktop configuration file (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "graylog": {
      "command": "/path/to/graylog-mcp",
      "env": {
        "GRAYLOG_URL": "https://graylog.example.com",
        "GRAYLOG_TOKEN": "your_access_token_here"
      }
    }
  }
}
```

Or with username/password:

```json
{
  "mcpServers": {
    "graylog": {
      "command": "/path/to/graylog-mcp",
      "env": {
        "GRAYLOG_URL": "https://graylog.example.com",
        "GRAYLOG_USERNAME": "admin",
        "GRAYLOG_PASSWORD": "secret"
      }
    }
  }
}
```

## Usage with Claude Code

```bash
claude mcp add graylog \
  --env GRAYLOG_URL=https://graylog.example.com \
  --env GRAYLOG_TOKEN=your_access_token_here \
  -- graylog-mcp
```

## Usage with n8n

Run the server in http mode using Docker Compose. No Graylog-specific config is needed on the server if you pass credentials per-request from n8n:

```yaml
services:
  graylog-mcp:
    image: ghcr.io/n0madic/graylog-mcp:latest
    environment:
      GRAYLOG_MCP_TRANSPORT: http
    ports:
      - "8090:8090"
```

Or set a server-level default URL if all pipelines point to the same Graylog instance:

```yaml
services:
  graylog-mcp:
    image: ghcr.io/n0madic/graylog-mcp:latest
    environment:
      GRAYLOG_MCP_TRANSPORT: http
      GRAYLOG_URL: https://graylog.internal
    ports:
      - "8090:8090"
```

**n8n MCP credential configuration:**

| Field | Value |
|---|---|
| MCP Server URL | `http://graylog-mcp:8090/mcp` |
| Header `X-Graylog-URL` | `https://graylog.example.com` (overrides server `GRAYLOG_URL`; omit if already set on the server) |
| Header `Authorization` | `Bearer <graylog_api_token>` or `Basic base64(username:password)` |

Each n8n pipeline uses its own credential with its own token or username — Graylog enforces per-user access rights on its side. The LLM only ever sees tool results.

## Tools

### `search_logs`

Search Graylog logs using Lucene query syntax.

**Parameters:**

| Name | Type | Required | Description |
|---|---|---|---|
| `query` | string | Yes | Lucene query (e.g. `level:ERROR AND service:auth`) |
| `stream_id` | string | No | Limit search to a specific stream |
| `range` | number | No | Relative time range in seconds (default: 300) |
| `from` | string | No | Absolute start time in ISO 8601 format |
| `to` | string | No | Absolute end time in ISO 8601 format |
| `limit` | number | No | Max messages to return (default: 50, max: 10000) |
| `offset` | number | No | Messages to skip for pagination |
| `fields` | string | No | Comma-separated list of fields to return |
| `sort` | string | No | Sort order (e.g. `timestamp:desc`) |
| `deduplicate` | boolean | No | Collapse duplicate messages and show count |
| `extract_templates` | boolean | No | Extract log templates using ULP pattern mining |

> `from` and `to` must be used together. If neither is set, a relative time range is used.
>
> When `deduplicate=true`, `limit` is applied to the number of deduplicated groups returned.
>
> `extract_templates` and `deduplicate` are mutually exclusive. When `extract_templates=true`, similar messages are grouped into templates with dynamic parts replaced by `<*>` wildcards (e.g. `"Connection to <*> failed: timeout"`). Returns templates sorted by frequency with counts and sample message IDs.

### `list_streams`

List available Graylog streams (excludes disabled streams).

**Parameters:**

| Name | Type | Required | Description |
|---|---|---|---|
| `title_filter` | string | No | Substring filter for stream titles (case-insensitive) |

### `list_fields`

List available log fields. Note: Graylog's API does not return field types.

**Parameters:**

| Name | Type | Required | Description |
|---|---|---|---|
| `name_filter` | string | No | Substring filter for field names (case-insensitive) |

### `aggregate_logs`

Aggregate logs using statistical functions with grouping. Uses Graylog's Scripting API.

**Parameters:**

| Name | Type | Required | Description |
|---|---|---|---|
| `query` | string | Yes | Lucene query (e.g. `level:ERROR AND service:auth`) |
| `metrics` | string | Yes | Comma-separated metrics (e.g. `count`, `avg:took_ms`, `percentile:took_ms:95`) |
| `group_by` | string | Yes | Comma-separated fields to group by (e.g. `source`, `source,level`) |
| `group_limit` | number | No | Max groups per field (default: 10) |
| `stream_id` | string | No | Limit aggregation to a specific stream |
| `range` | number | No | Relative time range in seconds (default: 300) |
| `from` | string | No | Absolute start time in ISO 8601 format |
| `to` | string | No | Absolute end time in ISO 8601 format |
| `sort` | string | No | Sort direction for the first metric: `asc` or `desc` |

> Supported metric functions: `count`, `avg`, `min`, `max`, `sum`, `stddev`, `variance`, `card`, `percentile`, `latest`, `sumofsquares`.
>
> `from`/`to` and `range` are mutually exclusive. If neither is set, a relative range of 300 seconds is used.

### `get_log_context`

Retrieve messages surrounding a specific log entry. Useful for understanding the sequence of events around an incident.

**Parameters:**

| Name | Type | Required | Description |
|---|---|---|---|
| `message_id` | string | Yes | The `_id` of the target message |
| `index` | string | Yes | The Elasticsearch index of the target message |
| `before` | number | No | Messages to fetch before the target (default: 5) |
| `after` | number | No | Messages to fetch after the target (default: 5) |
| `fields` | string | No | Comma-separated list of fields to return |
| `stream_id` | string | No | Restrict context search to a specific stream |

Response includes `context_incomplete: true` when fewer messages were found than requested (e.g. at beginning/end of log stream or due to response size limits). Messages are automatically deduplicated by ID with overfetch to fill context windows.

### Response fitting

All tools automatically fit responses within a 50,000-byte limit. When a response exceeds this limit, the server progressively truncates message text and reduces message count. A `response_truncated: true` flag is added when any truncation occurs. Use the `fields` parameter to select specific fields and reduce payload size.

## Example prompts

Once connected, you can ask your LLM things like:

- "Show me all ERROR logs from the last hour"
- "Search for authentication failures in the auth-service stream"
- "Find logs containing 'OutOfMemoryError' from the last 24 hours"
- "Show me the context around this log message: [message_id]"
- "What fields are available in my Graylog instance?"
- "List all streams related to payments"
- "Show deduplicated error logs from production to find the most common issues"
- "Extract log templates from the last hour to see the most common log patterns"
- "Count logs per source for the last hour and show the top 5"
- "What is the average response time grouped by service over the last 30 minutes?"
- "Show me the 95th percentile of request duration grouped by endpoint"

## License

MIT
