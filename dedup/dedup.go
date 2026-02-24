package dedup

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/n0madic/graylog-mcp/graylog"
)

type DedupResult struct {
	Message    graylog.Message `json:"message"`
	Index      string          `json:"index"`
	Count      int             `json:"count"`
	MessageIDs []string        `json:"message_ids"`
}

func (d DedupResult) MarshalJSON() ([]byte, error) {
	// Build message map without _id (redundant with message_ids)
	msgMap := make(map[string]any)
	msgMap["timestamp"] = d.Message.Timestamp
	msgMap["source"] = d.Message.Source
	msgMap["message"] = d.Message.Message
	for k, v := range d.Message.Extra {
		msgMap[k] = v
	}

	type alias struct {
		Message    map[string]any `json:"message"`
		Index      string         `json:"index"`
		Count      int            `json:"count"`
		MessageIDs []string       `json:"message_ids"`
	}

	return json.Marshal(alias{
		Message:    msgMap,
		Index:      d.Index,
		Count:      d.Count,
		MessageIDs: d.MessageIDs,
	})
}

func CapMessageIDs(results []DedupResult, maxIDs int) {
	for i := range results {
		if len(results[i].MessageIDs) > maxIDs {
			results[i].MessageIDs = results[i].MessageIDs[:maxIDs]
		}
	}
}

func Deduplicate(messages []graylog.MessageWrapper, hashFields []string) []DedupResult {
	seen := make(map[string]int) // hash -> index in results
	var results []DedupResult

	for _, mw := range messages {
		h := hashMessage(mw.Message, hashFields)
		if idx, ok := seen[h]; ok {
			results[idx].Count++
			results[idx].MessageIDs = append(results[idx].MessageIDs, mw.Message.ID)
		} else {
			seen[h] = len(results)
			results = append(results, DedupResult{
				Message:    mw.Message,
				Index:      mw.Index,
				Count:      1,
				MessageIDs: []string{mw.Message.ID},
			})
		}
	}

	return results
}

func hashMessage(msg graylog.Message, hashFields []string) string {
	h := sha256.New()

	if len(hashFields) > 0 {
		data := make(map[string]any)
		all := messageToMap(msg)
		for _, f := range hashFields {
			if v, ok := all[f]; ok {
				data[f] = v
			}
		}
		marshalToHash(h, sortedMap(data))
	} else {
		all := messageToMap(msg)
		filtered := make(map[string]any)
		for k, v := range all {
			if shouldSkipField(k) {
				continue
			}
			filtered[k] = v
		}
		marshalToHash(h, sortedMap(filtered))
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

func marshalToHash(h io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		h.Write([]byte(fmt.Sprintf("%v", v))) //nolint:errcheck
		return
	}
	h.Write(b) //nolint:errcheck
}

// messageToMap builds a flat map from a graylog.Message for hashing.
// It intentionally accesses struct fields directly rather than calling Message
// methods (MarshalJSON/ToFilteredMap) to avoid introducing a dependency on
// graylog package internals beyond the Message type itself — keeping this package
// focused on deduplication logic. Only source, message, and Extra are included;
// _id and timestamp are excluded from hashing via shouldSkipField.
func messageToMap(msg graylog.Message) map[string]any {
	result := map[string]any{
		"source":  msg.Source,
		"message": msg.Message,
	}
	for k, v := range msg.Extra {
		result[k] = v
	}
	return result
}

func shouldSkipField(field string) bool {
	return field == "_id" || field == "timestamp" || field == "index"
}

// sortedMap returns a deterministic representation for hashing.
func sortedMap(m map[string]any) [][2]any {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([][2]any, len(keys))
	for i, k := range keys {
		result[i] = [2]any{k, m[k]}
	}
	return result
}
