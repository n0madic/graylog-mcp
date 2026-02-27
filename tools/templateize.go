package tools

import (
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/n0madic/go-ulp"
	"github.com/n0madic/graylog-mcp/graylog"
)

// TemplateResult represents a single log template extracted by ULP.
type TemplateResult struct {
	Template   string   `json:"template"`
	Count      int      `json:"count"`
	MessageIDs []string `json:"message_ids"`
}

// templateizeMessages extracts log templates from messages using ULP pattern mining.
// It returns templates sorted by count (most frequent first).
func templateizeMessages(messages []graylog.MessageWrapper) ([]TemplateResult, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	// Build lines for ULP parser, mapping line index → message ID.
	lines := make([]string, len(messages))
	messageIDs := make([]string, len(messages))
	for i, mw := range messages {
		// ULP reads line-by-line; replace newlines so each message stays on one line.
		line := strings.ReplaceAll(mw.Message.Message, "\n", " ")
		line = strings.ReplaceAll(line, "\r", " ")
		lines[i] = line
		messageIDs[i] = mw.Message.ID
	}

	input := strings.NewReader(strings.Join(lines, "\n"))
	parser, err := ulp.New()
	if err != nil {
		return nil, err
	}
	parsed, err := parser.Parse(input)
	if err != nil {
		return nil, err
	}

	// Build group EventID → []LineID lookup for fast template→message mapping.
	groupEvents := make(map[string][]int, len(parsed.Groups))
	for _, g := range parsed.Groups {
		for _, ev := range g.Events {
			groupEvents[g.EventID] = append(groupEvents[g.EventID], ev.LineID)
		}
	}

	results := make([]TemplateResult, 0, len(parsed.Templates))
	for _, tmpl := range parsed.Templates {
		ids := make([]string, 0)
		for _, eid := range tmpl.EventIDs {
			for _, lineID := range groupEvents[eid] {
				if lineID >= 0 && lineID < len(messageIDs) {
					ids = append(ids, messageIDs[lineID])
				}
			}
		}

		results = append(results, TemplateResult{
			Template:   tmpl.Template,
			Count:      tmpl.Count,
			MessageIDs: ids,
		})
	}

	// Sort by count descending (most frequent first).
	sort.Slice(results, func(i, j int) bool {
		return results[i].Count > results[j].Count
	})

	return results, nil
}

// capTemplateMessageIDs caps the MessageIDs slice on each template to maxIDs.
func capTemplateMessageIDs(results []TemplateResult, maxIDs int) {
	for i := range results {
		if len(results[i].MessageIDs) > maxIDs {
			results[i].MessageIDs = results[i].MessageIDs[:maxIDs]
		}
	}
}

// fitTemplateSearchResult applies progressive fitting to a templateized search result.
func fitTemplateSearchResult(result map[string]any, maxSize int) (*mcp.CallToolResult, error) {
	adapter := resultAdapter{
		truncateMsgs: func(maxLen int) {
			if templates, ok := result["templates"].([]TemplateResult); ok {
				for i := range templates {
					templates[i].Template = truncateString(templates[i].Template, maxLen)
				}
			}
		},
		reduceMsgs: func() bool {
			templates, ok := result["templates"].([]TemplateResult)
			if !ok || len(templates) <= 1 {
				return false
			}
			newCount := len(templates) / 2
			if newCount < 1 {
				newCount = 1
			}
			result["templates"] = templates[:newCount]
			return true
		},
		lastResort: func() map[string]any {
			return map[string]any{
				"total_results":      result["total_results"],
				"template_count":     result["template_count"],
				"response_truncated": true,
				"error":              "Response too large even after truncation. Use 'fields' parameter or reduce the search scope.",
			}
		},
	}

	return fitResult(result, maxSize, adapter)
}
