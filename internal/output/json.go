// Package output provides formatters for writing QuotaResults
// in JSON and human-readable text formats.
package output

import (
	"encoding/json"
	"io"

	"github.com/schnetlerr/agent-quota/internal/provider"
)

// WriteJSON writes one or more QuotaResults as JSON to w.
// Single result produces a plain JSON object. Multiple results produce a JSON array.
func WriteJSON(w io.Writer, results []provider.QuotaResult) error {
	enc := json.NewEncoder(w)
	if len(results) == 1 {
		return enc.Encode(results[0])
	}
	return enc.Encode(results)
}
