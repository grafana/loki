package log

import (
	"fmt"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logql/log/xmlexpr"
)

// XMLExpressionParser extracts specified fields from XML logs and creates labels for them
type XMLExpressionParser struct {
	ids    []string      // Label names to create
	exprs  []*xmlexpr.Expr // Parsed XML expressions
	keys   internedStringSet
}

// NewXMLExpressionParser creates a new XML expression parser
func NewXMLExpressionParser(expressions []LabelExtractionExpr) (*XMLExpressionParser, error) {
	var ids []string
	var exprs []*xmlexpr.Expr

	for _, exp := range expressions {
		// Parse the XML path expression
		parsedExpr, err := xmlexpr.Parse(exp.Expression)
		if err != nil {
			return nil, fmt.Errorf("cannot parse xml expression [%s]: %w", exp.Expression, err)
		}

		// Validate the identifier
		if !model.UTF8Validation.IsValidLabelName(exp.Identifier) {
			return nil, fmt.Errorf("invalid extracted label name '%s'", exp.Identifier)
		}

		ids = append(ids, exp.Identifier)
		exprs = append(exprs, parsedExpr)
	}

	return &XMLExpressionParser{
		ids:   ids,
		exprs: exprs,
		keys:  internedStringSet{},
	}, nil
}

// Process extracts XML fields according to the configured expressions
func (x *XMLExpressionParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	if len(line) == 0 || lbs.ParserLabelHints().NoLabels() {
		return line, true
	}

	// Check that the line starts correctly for XML
	if !isValidXMLStart(line) {
		addErrLabel(errXML, nil, lbs)
		return line, true
	}

	// Extract fields using XML expressions
	for i, expr := range x.exprs {
		identifier := x.ids[i]

		// Extract values from XML
		values, err := expr.Extract(line)
		if err != nil {
			addErrLabel(errXML, err, lbs)
			// Still add empty label for this field
			x.setLabel(lbs, identifier, "")
			continue
		}

		// Take the first matching value (if multiple matches, we only use the first)
		value := ""
		if len(values) > 0 {
			value = values[0]
		}

		x.setLabel(lbs, identifier, value)
	}

	return line, true
}

// setLabel adds a label, handling duplicates by adding "_extracted" suffix
func (x *XMLExpressionParser) setLabel(lbs *LabelsBuilder, identifier, value string) {
	key, _ := x.keys.Get(unsafeGetBytes(identifier), func() (string, bool) {
		return identifier, true
	})

	if lbs.BaseHas(key) || lbs.HasInCategory(key, StructuredMetadataLabel) {
		key = key + duplicateSuffix
	}

	lbs.Set(ParsedLabel, key, value)
}

// RequiredLabelNames returns the label names that this parser extracts
func (x *XMLExpressionParser) RequiredLabelNames() []string { return []string{} }

// isValidXMLStart checks if the line looks like valid XML
func isValidXMLStart(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	// XML should start with '<'
	return data[0] == '<'
}
