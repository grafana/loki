package syntax

import (
	"fmt"
	"strings"
)

// Label represents a key-value pair for Loki labels.
// This is a simplified version that avoids Prometheus dependencies.
type Label struct {
	Name  string
	Value string
}

// Labels is a slice of Label pairs, sorted by name.
type Labels []Label

// String returns the string representation of labels in LogQL format.
func (ls Labels) String() string {
	if len(ls) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, l := range ls {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(l.Name)
		b.WriteByte('=')
		b.WriteByte('"')
		b.WriteString(escapeLabelValue(l.Value))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// Get returns the value for the given label name, or empty string if not found.
func (ls Labels) Get(name string) string {
	for _, l := range ls {
		if l.Name == name {
			return l.Value
		}
	}
	return ""
}

// Has returns true if the label with the given name exists.
func (ls Labels) Has(name string) bool {
	return ls.Get(name) != ""
}

// EmptyLabels returns an empty Labels slice.
func EmptyLabels() Labels {
	return Labels{}
}

// escapeLabelValue escapes special characters in label values.
func escapeLabelValue(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ParseLabels parses a LogQL label selector string and returns Labels.
// This is a simplified parser that handles basic label selectors like {foo="bar",baz="qux"}.
// For full LogQL parsing including regex matchers, use the full syntax parser.
func ParseLabels(lbs string) (Labels, error) {
	lbs = strings.TrimSpace(lbs)
	if lbs == "" || lbs == "{}" {
		return EmptyLabels(), nil
	}

	// Remove braces
	if !strings.HasPrefix(lbs, "{") || !strings.HasSuffix(lbs, "}") {
		return nil, fmt.Errorf("labels must be wrapped in braces: %q", lbs)
	}
	lbs = lbs[1 : len(lbs)-1]

	var labels Labels
	parts := splitLabelString(lbs)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		eqIdx := strings.Index(part, "=")
		if eqIdx == -1 {
			return nil, fmt.Errorf("invalid label format: %q (missing =)", part)
		}

		name := strings.TrimSpace(part[:eqIdx])
		value := strings.TrimSpace(part[eqIdx+1:])

		// Remove quotes if present
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
			// Unescape
			value = unescapeLabelValue(value)
		}

		if name == "" {
			return nil, fmt.Errorf("label name cannot be empty")
		}

		labels = append(labels, Label{Name: name, Value: value})
	}

	return labels, nil
}

// splitLabelString splits a label string by commas, respecting quoted values.
func splitLabelString(s string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false
	escapeNext := false

	for _, r := range s {
		if escapeNext {
			current.WriteRune(r)
			escapeNext = false
			continue
		}

		if r == '\\' {
			escapeNext = true
			current.WriteRune(r)
			continue
		}

		if r == '"' {
			inQuotes = !inQuotes
			current.WriteRune(r)
			continue
		}

		if r == ',' && !inQuotes {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// unescapeLabelValue unescapes special characters in label values.
func unescapeLabelValue(s string) string {
	var b strings.Builder
	escapeNext := false
	for _, r := range s {
		if escapeNext {
			switch r {
			case 'n':
				b.WriteByte('\n')
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			default:
				b.WriteRune(r)
			}
			escapeNext = false
			continue
		}
		if r == '\\' {
			escapeNext = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
