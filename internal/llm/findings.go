package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Severity tags are collected from day one even though v1 never fails CI on
// them — avoids re-teaching the model when blocking mode lands.
const (
	SeverityBlock = "block"
	SeverityWarn  = "warn"
	SeverityNit   = "nit"
)

// Finding is one review comment reported by the model.
type Finding struct {
	File     string `json:"file"`
	Line     int    `json:"line"` // new-file line number as shown in the diff
	Severity string `json:"severity"`
	Comment  string `json:"comment"`
}

// ParseFindings extracts the JSON findings array from a model response,
// tolerating Markdown code fences and prose around the array.
func ParseFindings(response string) ([]Finding, error) {
	text := strings.TrimSpace(response)

	// Strip a ```json ... ``` fence if present.
	if strings.HasPrefix(text, "```") {
		if _, rest, ok := strings.Cut(text, "\n"); ok {
			text = rest
		}
		if idx := strings.LastIndex(text, "```"); idx >= 0 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
	}

	// Fall back to the outermost [...] span if prose surrounds the array.
	if !strings.HasPrefix(text, "[") {
		start := strings.Index(text, "[")
		end := strings.LastIndex(text, "]")
		if start == -1 || end <= start {
			return nil, fmt.Errorf("no JSON array in model response: %s", truncate(text, 300))
		}
		text = text[start : end+1]
	}

	var findings []Finding
	if err := json.Unmarshal([]byte(text), &findings); err != nil {
		return nil, fmt.Errorf("parse findings JSON: %w", err)
	}

	valid := findings[:0]
	for _, f := range findings {
		if f.File == "" || f.Line <= 0 || f.Comment == "" {
			continue
		}
		switch f.Severity {
		case SeverityBlock, SeverityWarn, SeverityNit:
		default:
			f.Severity = SeverityWarn
		}
		valid = append(valid, f)
	}
	return valid, nil
}
