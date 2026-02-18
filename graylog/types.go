package graylog

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
)

// isHiddenField returns true for internal Graylog metadata fields
// that should be excluded from tool responses to reduce noise.
func isHiddenField(key string) bool {
	return strings.HasPrefix(key, "gl2_")
}

// isHiddenValue returns true for placeholder values that carry no useful information.
func isHiddenValue(v any) bool {
	s, ok := v.(string)
	return ok && s == "fullyCutByExtractor"
}

type SearchParams struct {
	Query           string
	Range           int    // seconds, for relative search
	From            string // ISO8601, for absolute search
	To              string // ISO8601, for absolute search
	Limit           int
	Offset          int
	Fields          string   // comma-separated
	Sort            string   // field:asc or field:desc
	StreamIDs       []string // filter by stream IDs
	TruncateMessage int      // 0 = no truncation, >0 = max chars for message field
}

type SearchResponse struct {
	Messages     []MessageWrapper `json:"messages"`
	TotalResults int              `json:"total_results"`
}

type MessageWrapper struct {
	Message Message `json:"message"`
	Index   string  `json:"index"`
}

type Message struct {
	ID        string         `json:"_id"`
	Timestamp string         `json:"timestamp"`
	Source    string         `json:"source"`
	Message   string         `json:"message"`
	Extra     map[string]any `json:"-"`
}

func (m *Message) UnmarshalJSON(data []byte) error {
	raw := make(map[string]any)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if v, ok := raw["_id"]; ok {
		m.ID, _ = v.(string)
	}
	if v, ok := raw["timestamp"]; ok {
		m.Timestamp, _ = v.(string)
	}
	if v, ok := raw["source"]; ok {
		m.Source, _ = v.(string)
	}
	if v, ok := raw["message"]; ok {
		m.Message, _ = v.(string)
	}

	populateExtra(m, raw)
	return nil
}

// populateExtra fills m.Extra with all non-core, non-hidden fields from raw.
func populateExtra(m *Message, raw map[string]any) {
	m.Extra = make(map[string]any)
	knownFields := map[string]bool{"_id": true, "timestamp": true, "source": true, "message": true}
	for k, v := range raw {
		if !knownFields[k] && !isHiddenField(k) && !isHiddenValue(v) {
			m.Extra[k] = v
		}
	}
}

func (m Message) MarshalJSON() ([]byte, error) {
	result := make(map[string]any)
	result["_id"] = m.ID
	result["timestamp"] = m.Timestamp
	result["source"] = m.Source
	result["message"] = m.Message
	for k, v := range m.Extra {
		result[k] = v
	}
	return json.Marshal(result)
}

// ToFilteredMap returns a map with only the requested fields.
// If fields is empty, all fields are returned.
// Core fields (_id, timestamp, source, message) are always included regardless of the filter.
func (m Message) ToFilteredMap(fields []string) map[string]any {
	result := map[string]any{
		"_id":       m.ID,
		"timestamp": m.Timestamp,
		"source":    m.Source,
		"message":   m.Message,
	}

	if len(fields) == 0 {
		maps.Copy(result, m.Extra)
		return result
	}

	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}
	for k, v := range m.Extra {
		if fieldSet[k] {
			result[k] = v
		}
	}
	return result
}

// messageFromMap constructs a Message directly from a map[string]any
// without going through a JSON marshal/unmarshal round-trip.
func messageFromMap(raw map[string]any) Message {
	var m Message
	if v, ok := raw["_id"]; ok {
		m.ID, _ = v.(string)
	}
	if v, ok := raw["timestamp"]; ok {
		m.Timestamp, _ = v.(string)
	}
	if v, ok := raw["source"]; ok {
		m.Source, _ = v.(string)
	}
	if v, ok := raw["message"]; ok {
		m.Message, _ = v.(string)
	}

	populateExtra(&m, raw)
	return m
}

type StreamsResponse struct {
	Streams []Stream `json:"streams"`
	Total   int      `json:"total"`
}

type Stream struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	IndexSetID  string `json:"index_set_id"`
	Disabled    bool   `json:"disabled"`
}

type FieldsResponse map[string]FieldInfo

type FieldInfo struct {
	FieldName string `json:"field_name"`
}

type APIError struct {
	StatusCode int
	Body       string
	Path       string
}

func (e *APIError) Error() string {
	body := e.Body
	if len(body) > 500 {
		body = body[:500] + "...[truncated]"
	}
	return fmt.Sprintf("Graylog API error: status=%d path=%s body=%s", e.StatusCode, e.Path, body)
}

// Views Search API request types (POST /api/views/search/sync)

type viewsSearchRequest struct {
	Queries []viewsQuery `json:"queries"`
}

type viewsQuery struct {
	ID          string            `json:"id"`
	TimeRange   viewsTimeRange    `json:"timerange"`
	Query       viewsBackendQuery `json:"query"`
	Filter      *viewsFilter      `json:"filter,omitempty"`
	SearchTypes []viewsSearchType `json:"search_types"`
}

type viewsTimeRange struct {
	Type  string `json:"type"`
	Range int    `json:"range,omitempty"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
}

type viewsBackendQuery struct {
	Type        string `json:"type"`
	QueryString string `json:"query_string"`
}

type viewsFilter struct {
	Type    string        `json:"type"`
	Filters []viewsFilter `json:"filters,omitempty"`
	ID      string        `json:"id,omitempty"`
}

type viewsSearchType struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
	Sort   []viewsSortItem `json:"sort,omitempty"`
	Fields []string        `json:"fields,omitempty"`
}

type viewsSortItem struct {
	Field string `json:"field"`
	Order string `json:"order"`
}

// Views Search API response types

type viewsSearchResponse struct {
	Results map[string]viewsQueryResult `json:"results"`
}

type viewsQueryResult struct {
	SearchTypes map[string]viewsSearchTypeResult `json:"search_types"`
}

type viewsSearchTypeResult struct {
	TotalResults int                  `json:"total_results"`
	Messages     []viewsResultMessage `json:"messages"`
}

type viewsResultMessage struct {
	Message         map[string]any `json:"message"`
	Index           string         `json:"index"`
	HighlightRanges map[string]any `json:"highlight_ranges"`
}

// Scripting API types (POST /api/search/aggregate)

type ScriptingTimeRange struct {
	Type    string `json:"type"`
	Range   int    `json:"range,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Keyword string `json:"keyword,omitempty"`
}

type ScriptingGrouping struct {
	Field string `json:"field"`
	Limit int    `json:"limit,omitempty"`
}

type ScriptingMetricConfig struct {
	Percentile float64 `json:"percentile"`
}

type ScriptingMetric struct {
	Function      string                 `json:"function"`
	Field         string                 `json:"field,omitempty"`
	Sort          string                 `json:"sort,omitempty"`
	Configuration *ScriptingMetricConfig `json:"configuration,omitempty"`
}

type ScriptingAggregateRequest struct {
	Query     string              `json:"query"`
	Streams   []string            `json:"streams,omitempty"`
	TimeRange ScriptingTimeRange  `json:"timerange"`
	GroupBy   []ScriptingGrouping `json:"group_by,omitempty"`
	Metrics   []ScriptingMetric   `json:"metrics"`
}

type ScriptingSchemaEntry struct {
	Field      string `json:"field,omitempty"`
	Function   string `json:"function,omitempty"`
	Name       string `json:"name"`
	ColumnType string `json:"column_type,omitempty"`
	Type       string `json:"type"`
}

type ScriptingMetadata struct {
	EffectiveTimerange map[string]any `json:"effective_timerange"`
}

type ScriptingTabularResponse struct {
	Schema   []ScriptingSchemaEntry `json:"schema"`
	DataRows [][]any                `json:"datarows"`
	Metadata ScriptingMetadata      `json:"metadata"`
}
