# Graylog MCP Server

An [MCP](https://modelcontextprotocol.io/) (Model Context Protocol) server that gives LLMs the ability to search and analyze logs from [Graylog](https://graylog.org/). Works with Claude Desktop, Claude Code, Cursor, and any MCP-compatible client.

Single binary, zero runtime dependencies. Written in Go.

## Features

- **Search logs** with Lucene query syntax, time ranges, pagination, and sorting
- **Aggregate logs** with statistical functions (count, avg, min, max, percentile, etc.) and grouping
- **Stream filtering** to scope searches to specific Graylog streams
- **Log deduplication** to collapse repeated messages and show counts
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
| `GRAYLOG_URL` | `--url` | Yes | - | Graylog base URL |
| `GRAYLOG_USERNAME` | `--username` | If no token | - | Username for Basic Auth |
| `GRAYLOG_PASSWORD` | `--password` | If no token | - | Password for Basic Auth |
| `GRAYLOG_TOKEN` | `--token` | If no credentials | - | API access token |
| `GRAYLOG_TLS_SKIP_VERIFY` | `--tls-skip-verify` | No | `false` | Skip TLS certificate verification |
| `GRAYLOG_TIMEOUT` | `--timeout` | No | `30s` | HTTP request timeout |

### Authentication

Two authentication methods are supported (at least one is required):

1. **Username & password** - standard Graylog credentials via Basic Auth
2. **API access token** - a Graylog access token (uses Basic Auth with `your_token:token` convention)

If both are provided, the token takes precedence.

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
| `truncate_message` | number | No | Truncate message text to N characters |
| `max_result_size` | number | No | Max response size in characters (default: 50000) |

> `from` and `to` must be used together. If neither is set, a relative time range is used.
>
> When `deduplicate=true`, `limit` is applied to the number of deduplicated groups returned.

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
| `timerange_keyword` | string | No | Natural language time range (e.g. `last five minutes`) |
| `sort` | string | No | Sort direction for the first metric: `asc` or `desc` |
| `max_result_size` | number | No | Max response size in characters (default: 50000) |

> Supported metric functions: `count`, `avg`, `min`, `max`, `sum`, `stddev`, `variance`, `card`, `percentile`, `latest`, `sumofsquares`.
>
> `from`/`to`, `range`, and `timerange_keyword` are mutually exclusive. If none is set, a relative range of 300 seconds is used.

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
| `deduplicate` | boolean | No | Deduplicate by message ID and enable overfetch for better context fill (default: `true`) |

`get_log_context` response also includes context fill metadata:

- `before_requested`, `after_requested` - requested context sizes
- `before_returned`, `after_returned` - actual returned message counts
- `context_incomplete` - `true` when fewer messages were found than requested

## Example prompts

Once connected, you can ask your LLM things like:

- "Show me all ERROR logs from the last hour"
- "Search for authentication failures in the auth-service stream"
- "Find logs containing 'OutOfMemoryError' from the last 24 hours"
- "Show me the context around this log message: [message_id]"
- "What fields are available in my Graylog instance?"
- "List all streams related to payments"
- "Show deduplicated error logs from production to find the most common issues"
- "Count logs per source for the last hour and show the top 5"
- "What is the average response time grouped by service over the last 30 minutes?"
- "Show me the 95th percentile of request duration grouped by endpoint"

## License

MIT
