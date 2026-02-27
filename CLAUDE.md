# CLAUDE.md — Graylog MCP Server

## What is this project

MCP (Model Context Protocol) server that gives LLMs the ability to search and retrieve logs from Graylog via its REST API. The server runs over stdio transport and is meant to be used with Claude Desktop, Claude Code, or any MCP-compatible client.

Written in Go. Single binary, zero runtime dependencies.

## Quick reference

```bash
# Build
go build -o graylog-mcp .

# Run with username/password
export GRAYLOG_URL=https://graylog.example.com
export GRAYLOG_USERNAME=admin
export GRAYLOG_PASSWORD=secret
./graylog-mcp

# Run with API access token (alternative to username/password)
export GRAYLOG_URL=https://graylog.example.com
export GRAYLOG_TOKEN=your_access_token_here
./graylog-mcp

# Run (flag-based config)
./graylog-mcp --url https://graylog.example.com --username admin --password secret
./graylog-mcp --url https://graylog.example.com --token your_access_token_here

# Verify
go vet ./...
go build ./...
```

Verify changes compile with `go build ./...`, pass `go vet ./...`, and `go test ./... -count=1`.

## Project structure

```
main.go                      Entry point: config -> client -> MCP server -> stdio
config/config.go             Env vars + CLI flags parsing, fail-fast validation
graylog/
  types.go                   API types: SearchParams, Message (custom JSON), Stream, Field, APIError, Scripting API types
  client.go                  HTTP client: Basic Auth, search (Views API), aggregate (Scripting API), streams, fields, message
dedup/dedup.go               SHA256-based log deduplication, custom MarshalJSON (omits _id), CapMessageIDs
tools/
  helpers.go                 toolSuccess/toolSuccessJSON/toolError response builders, param extraction
  fit_result.go              Generic progressive response fitting (resultAdapter + fitResult)
  search_logs.go             search_logs tool + executeSearch (optional stream_id for stream filtering, extract_templates for ULP templateization)
  templateize.go             ULP-based log templateization: templateizeMessages, capTemplateMessageIDs, fitTemplateSearchResult
  list_streams.go            list_streams tool (filters disabled, optional title substring filter)
  list_fields.go             list_fields tool (optional name substring filter, sorted []string output — no types, API doesn't return them)
  get_log_context.go         get_log_context tool (fetches messages before/after a target by timestamp, optional stream_id filter)
  aggregate_logs.go          aggregate_logs tool (Scripting API aggregations: count, avg, min, max, percentile, etc. with group_by)
  register.go                RegisterAll — wires all 5 tools to MCP server
```

## Architecture & data flow

```
main.go
  config.Load()           parse env/flags, fail if GRAYLOG_URL + auth missing
  graylog.NewClient()     HTTP client with Basic Auth (credentials or token), TLS config, timeout
  server.NewMCPServer()   MCP server from mark3labs/mcp-go
  tools.RegisterAll()     register all 5 tools with handlers that close over the client
  server.ServeStdio()     blocks, reads JSON-RPC from stdin, writes to stdout
```

Each tool handler:
1. Extracts params from `request.Params.Arguments` (a `map[string]any`)
2. Validates required params, returns `toolError()` (IsError: true) for validation failures
3. Calls `graylog.Client` methods
4. Returns `toolSuccess()` (JSON-serialized result) or `toolError()` for API errors
5. Never returns a Go `error` from the handler — all application errors go through `toolError()`

## Key conventions

### Error handling
- API errors (`*graylog.APIError`) are type-asserted and returned as `toolError()` with status/path/body
- Network/parse errors are returned as `toolError("descriptive message: " + err.Error())`
- Tool handlers always return `(*mcp.CallToolResult, nil)` — never `(nil, error)`
- Config validation is fail-fast: missing required env/flags cause immediate `os.Exit(1)`

### Parameter access
- All tool params come from `request.Params.Arguments` (`map[string]any`)
- Use helpers from `tools/helpers.go`: `getStringParam`, `getStrictNonNegativeIntParam`, `getBoolParam`
- JSON numbers arrive as `float64` — `getStrictNonNegativeIntParam` handles `float64`, `int`, and `json.Number`
- Defaults are handled in the helper calls or after extraction (e.g. limit defaults to 50)

### Message type
- `graylog.Message` has custom `UnmarshalJSON`/`MarshalJSON` — known fields (_id, timestamp, source, message) are struct fields, everything else goes into `Extra map[string]any`
- This preserves arbitrary Graylog fields while keeping typed access to core fields
- `populateExtra(m *Message, raw map[string]any)` is the shared helper used by both `UnmarshalJSON` and `messageFromMap` to fill `Extra` — update only this one place when adding new hidden/known fields

### Search routing
- `client.Search()` builds a Views API request (`POST /api/views/search/sync`): if `from` AND `to` are set → absolute timerange, otherwise → relative timerange
- Default relative range is 300 seconds (5 minutes)
- Limit is capped at 10000 (Elasticsearch limitation)
- Stream filtering is done via `StreamIDs` field in `SearchParams`, translated to Views filter objects

### Aggregation (Scripting API)
- `client.Aggregate()` posts to `/api/search/aggregate` (Scripting API) — separate from Views API used by search
- `aggregate_logs` accepts metrics as a comma-separated string parsed into `[]ScriptingMetric`: `"count"`, `"avg:field"`, `"percentile:field:value"`
- `group_by` is required — Graylog's Scripting API rejects requests without groupings
- Time range supports two modes: `from`/`to` (absolute ISO8601) or `range` (relative seconds, default 300)
- Tabular response (`schema` + `datarows`) is converted to array of named objects for LLM readability
- Fitting uses `fitResult()` with row-halving reduction; no message truncation phase (aggregation rows have no message bodies)

### Deduplication
- SHA256 hash of message content excluding `_id`, `timestamp`, `index` fields
- Hash is always computed over **all** fields — `fieldList` (the `fields` output filter) is never passed as `hashFields`; `dedup.Deduplicate` is always called with `nil` for `hashFields`
- Map keys are sorted before hashing for determinism
- Result preserves first occurrence order, aggregates count and message IDs
- `DedupResult` has custom `MarshalJSON` that omits `_id` from the message (redundant with `message_ids`)
- `DedupResult.Count` is the authoritative total occurrence count; `message_ids` is capped to 5 but `count` always reflects the full number
- `CapMessageIDs(results, 5)` is applied immediately after `Deduplicate`, before `fitResult` — the cap is always enforced
- Dedup response key is `total_raw_results` (not `total_results`) to signal it is the raw Graylog match count, not the unique-group count
- Dedup response key `unique_in_batch` is the count of unique groups in the fetched batch (not a global unique count)

### Templateization (ULP)
- `extract_templates` param on `search_logs` enables ULP-based log pattern mining via `github.com/n0madic/go-ulp`
- Mutually exclusive with `deduplicate` — templateization subsumes dedup (groups similar, not just identical)
- Newlines in messages are replaced with spaces before feeding to ULP (it reads line-by-line)
- `MessageIDs` capped to 5 per template (same convention as dedup)
- Templates sorted by count descending (most frequent patterns first)
- Overfetch strategy same as dedup — `limit * 3` for better template coverage
- Fitting: truncate template strings → halve template count → metadata-only last resort

### Tool responses
- `toolSuccess(data)` serializes with `json.Marshal` to JSON text
- `toolSuccessJSON(data []byte)` wraps pre-serialized JSON (avoids double-marshal after fitting)
- `toolError(msg)` sets `IsError: true` with text content
- Search results include `has_more` boolean for pagination awareness

### Response size fitting
- All tools use hardcoded `defaultMaxResultSize` (50000 bytes) — defined in `tools/helpers.go`
- Generic fitting algorithm in `fitResult()` (`tools/fit_result.go`) with `resultAdapter` callbacks:
  - Phase 1: Progressive message truncation (500 → 200 → 100 → 50 chars)
  - Phase 2: Halve message count repeatedly (`reduceMsgs` returns `false` when can't reduce further)
  - Last resort (search only): metadata-only response with hint to use `fields` parameter
- `response_truncated: true` flag added when any truncation occurs
- Dedup `message_ids` capping (max 5) is done **before** `fitResult`, not inside it — `resultAdapter` has no `capIDs` phase
- `get_log_context` `reduceMsgs` sets `context_incomplete = true` whenever it reduces the message window, so `context_incomplete` and `response_truncated` stay consistent
- `get_log_context` always deduplicates by message ID and overfetches to fill context windows

## MCP SDK

Uses `github.com/mark3labs/mcp-go` v0.44.0:
- `server.NewMCPServer(name, version, opts...)` — creates server
- `s.AddTool(tool, handler)` — registers a tool
- `mcp.NewTool(name, opts...)` — defines tool schema with `mcp.WithString/WithNumber/WithBoolean`
- `mcp.Required()`, `mcp.Description()` — parameter options
- `server.ServeStdio(s)` — stdio transport
- Handler signature: `func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)`

## Adding a new tool

1. Create `tools/new_tool.go` with:
   - `newToolNameTool() mcp.Tool` — tool definition with params
   - `newToolNameHandler(client *graylog.Client) func(ctx, request) (*mcp.CallToolResult, error)` — handler factory
2. Register in `tools/register.go`: `s.AddTool(newToolNameTool(), newToolNameHandler(client))`
3. If new Graylog API endpoint needed, add method to `graylog/client.go` and types to `graylog/types.go`

Follow the pattern of existing tools — each file is self-contained with tool definition + handler.

## Adding a new Graylog API endpoint

1. Add response types to `graylog/types.go`
2. Add method to `graylog/client.go` using `c.doGet(ctx, path, params)` or `c.doPost(ctx, path, body)`
3. `doGet`/`doPost` handle: Basic Auth, required headers, error status codes → `*APIError`

## Configuration

| Env var | CLI flag | Required | Default | Description |
|---------|----------|----------|---------|-------------|
| `GRAYLOG_URL` | `--url` | stdio: yes; http: no | — | Graylog base URL (http mode: override per-request via `X-Graylog-URL` header) |
| `GRAYLOG_USERNAME` | `--username` | stdio only, if no token | — | Basic auth username |
| `GRAYLOG_PASSWORD` | `--password` | stdio only, if no token | — | Basic auth password |
| `GRAYLOG_TOKEN` | `--token` | stdio only, if no user/pass | — | API access token (alternative to username/password) |
| `GRAYLOG_TLS_SKIP_VERIFY` | `--tls-skip-verify` | no | false | Skip TLS verification |
| `GRAYLOG_TIMEOUT` | `--timeout` | no | 30s | HTTP request timeout |
| `GRAYLOG_MCP_TRANSPORT` | `--transport` | no | stdio | Transport: `stdio` or `http` |
| `GRAYLOG_MCP_HTTP_BIND` | `--bind` | no | 0.0.0.0:8090 | HTTP listen address (http transport only) |

CLI flags override env vars.

### Authentication modes

Two mutually exclusive authentication methods are supported:

1. **Username/password** — `GRAYLOG_USERNAME` + `GRAYLOG_PASSWORD` → Basic Auth with those credentials
2. **API access token** — `GRAYLOG_TOKEN` → Basic Auth with `token_value:token` (Graylog convention)

If a token is provided, it takes precedence. At least one method must be configured or the server exits immediately.

## Graylog API endpoints used

| Method | Path | Used by |
|--------|------|---------|
| POST | `/api/views/search/sync` | search_logs, get_log_context |
| POST | `/api/search/aggregate` | aggregate_logs |
| GET | `/api/streams` | list_streams |
| GET | `/api/system/fields` | list_fields |
| GET | `/api/messages/{index}/{messageId}` | get_log_context |

All requests include: `Accept: application/json`, `X-Requested-By: XMLHttpRequest`, Basic Auth header.

## Common pitfalls

- `from` and `to` must both be set or both empty — partial is a validation error
- Graylog's `/api/system/fields` returns `{"fields": ["name1", "name2", ...]}` (stringArrayMap — array of strings, no types) — `GetFields` builds a `FieldsResponse` map with only `FieldName`, `PhysicalType` is absent
- `Message.Extra` is `json:"-"` — custom marshal/unmarshal handles it, don't add json tags
- Stream filtering via optional `stream_id` param in `search_logs`, `get_log_context`, and `aggregate_logs` — Views tools use `StreamIDs` in `SearchParams` (filter objects), `aggregate_logs` uses `Streams` field in `ScriptingAggregateRequest`
- `get_log_context` uses epoch boundaries (`1970-01-01` / `2099-12-31`) for before/after searches and filters out the target message by ID; optional `stream_id` restricts context to a specific stream
- `/api/messages/{index}/{id}` returns `{"message": {"fields": {actual data including _id}, ...metadata...}, "index": "..."}` — real message fields are nested inside `message.fields`, not at the top level; `GetMessage` extracts and re-unmarshals them into `Message`
- Token auth uses Basic Auth with `token_value` as username and literal `"token"` as password — this is a Graylog convention, not a custom scheme
- If both `GRAYLOG_TOKEN` and `GRAYLOG_USERNAME`/`GRAYLOG_PASSWORD` are set, token takes precedence
- `DedupResult.Message` is `graylog.Message` internally but `MarshalJSON` omits `_id` — don't rely on `_id` in serialized dedup output
- `ToFilteredMap(fieldList)` always includes core fields (`_id`, `timestamp`, `source`, `message`) regardless of `fieldList` — extra fields are filtered; this makes non-dedup field filtering consistent with the dedup path
- Non-dedup search results always use `ToFilteredMap(fieldList)` for uniform `map[string]any` — enables post-processing in `truncateMessagesInResult`
- `resultAdapter.reduceMsgs` must return `false` when no further reduction is possible to prevent infinite loops in `fitResult`
- Scripting API's `/api/search/aggregate` requires at least one grouping — `group_by` is mandatory in `aggregate_logs`
- `aggregate_logs` metrics string parsing: `"count"` (no field), `"avg:field"` (function:field), `"percentile:field:value"` (function:field:config) — validated against a known function set
