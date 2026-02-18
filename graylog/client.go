package graylog

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

func NewClient(baseURL, username, password string, tlsSkipVerify bool, timeout time.Duration) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: tlsSkipVerify} //nolint:gosec
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

func (c *Client) doGet(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return nil, fmt.Errorf("building request URL: %w", err)
	}
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-By", "XMLHttpRequest")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
			Path:       path,
		}
	}

	return body, nil
}

func (c *Client) doPost(ctx context.Context, path string, body any) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request body: %w", err)
	}

	u, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return nil, fmt.Errorf("building request URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-By", "XMLHttpRequest")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
			Path:       path,
		}
	}

	return respBody, nil
}

func (c *Client) Search(ctx context.Context, params SearchParams) (*SearchResponse, error) {
	// Build time range
	var tr viewsTimeRange
	if params.From != "" && params.To != "" {
		tr = viewsTimeRange{Type: "absolute", From: params.From, To: params.To}
	} else {
		r := params.Range
		if r == 0 {
			r = 300
		}
		tr = viewsTimeRange{Type: "relative", Range: r}
	}

	// Build filter for stream IDs
	var filter *viewsFilter
	if len(params.StreamIDs) > 0 {
		streamFilters := make([]viewsFilter, len(params.StreamIDs))
		for i, id := range params.StreamIDs {
			streamFilters[i] = viewsFilter{Type: "stream", ID: id}
		}
		filter = &viewsFilter{Type: "or", Filters: streamFilters}
	}

	// Build sort
	var sortItems []viewsSortItem
	if params.Sort != "" {
		parts := strings.SplitN(params.Sort, ":", 2)
		if len(parts) == 2 {
			sortItems = []viewsSortItem{{Field: parts[0], Order: strings.ToUpper(parts[1])}}
		}
	}

	// Build fields list
	var fields []string
	if params.Fields != "" {
		for _, f := range strings.Split(params.Fields, ",") {
			fields = append(fields, strings.TrimSpace(f))
		}
	}

	limit := params.Limit
	if limit == 0 {
		limit = 50
	}

	reqBody := viewsSearchRequest{
		Queries: []viewsQuery{{
			ID:        "q1",
			TimeRange: tr,
			Query:     viewsBackendQuery{Type: "elasticsearch", QueryString: params.Query},
			Filter:    filter,
			SearchTypes: []viewsSearchType{{
				ID:     "msgs",
				Type:   "messages",
				Limit:  limit,
				Offset: params.Offset,
				Sort:   sortItems,
				Fields: fields,
			}},
		}},
	}

	data, err := c.doPost(ctx, "/api/views/search/sync", reqBody)
	if err != nil {
		return nil, err
	}

	var viewsResp viewsSearchResponse
	if err := json.Unmarshal(data, &viewsResp); err != nil {
		return nil, fmt.Errorf("parsing views search response: %w", err)
	}

	// Extract results from Views response
	queryResult, ok := viewsResp.Results["q1"]
	if !ok {
		return &SearchResponse{}, nil
	}
	searchTypeResult, ok := queryResult.SearchTypes["msgs"]
	if !ok {
		return &SearchResponse{}, nil
	}

	// Convert viewsResultMessage → MessageWrapper directly from map
	messages := make([]MessageWrapper, len(searchTypeResult.Messages))
	for i, vrm := range searchTypeResult.Messages {
		messages[i] = MessageWrapper{
			Message: messageFromMap(vrm.Message),
			Index:   vrm.Index,
		}
	}

	return &SearchResponse{
		Messages:     messages,
		TotalResults: searchTypeResult.TotalResults,
	}, nil
}

func (c *Client) GetStreams(ctx context.Context) (*StreamsResponse, error) {
	data, err := c.doGet(ctx, "/api/streams", nil)
	if err != nil {
		return nil, err
	}

	var resp StreamsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing streams response: %w", err)
	}
	return &resp, nil
}

func (c *Client) GetFields(ctx context.Context) (FieldsResponse, error) {
	data, err := c.doGet(ctx, "/api/system/fields", nil)
	if err != nil {
		return nil, err
	}

	// API returns stringArrayMap: {"fields": ["name1", "name2", ...]}
	var wrapper struct {
		Fields []string `json:"fields"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing fields response: %w", err)
	}

	resp := make(FieldsResponse)
	for _, name := range wrapper.Fields {
		resp[name] = FieldInfo{FieldName: name}
	}
	return resp, nil
}

func (c *Client) Aggregate(ctx context.Context, req ScriptingAggregateRequest) (*ScriptingTabularResponse, error) {
	data, err := c.doPost(ctx, "/api/search/aggregate", req)
	if err != nil {
		return nil, err
	}

	var resp ScriptingTabularResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing aggregate response: %w", err)
	}
	return &resp, nil
}

func (c *Client) GetMessage(ctx context.Context, index, messageID string) (*MessageWrapper, error) {
	path := fmt.Sprintf("/api/messages/%s/%s", url.PathEscape(index), url.PathEscape(messageID))
	data, err := c.doGet(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	// /api/messages/{index}/{id} returns:
	// {"message": {"fields": {actual _id, timestamp, source, message, ...}, ...metadata...}, "index": "..."}
	// The actual message data is nested inside message.fields, not at the top level.
	var raw struct {
		Message struct {
			Fields map[string]any `json:"fields"`
		} `json:"message"`
		Index string `json:"index"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing message response: %w", err)
	}

	fieldsJSON, err := json.Marshal(raw.Message.Fields)
	if err != nil {
		return nil, fmt.Errorf("re-marshaling message fields: %w", err)
	}

	var msg Message
	if err := json.Unmarshal(fieldsJSON, &msg); err != nil {
		return nil, fmt.Errorf("parsing message fields: %w", err)
	}

	return &MessageWrapper{Message: msg, Index: raw.Index}, nil
}
