package json_expr

import (
	"strings"
)

func init() {
	JSONExprErrorVerbose = true
}

func ParseJSONExpression(expr string, debug bool) ([]interface{}, error) {
	s := NewScanner(strings.NewReader(expr), debug)
	JSONExprParse(s)

	if s.err != nil {
		return nil, s.err
	}
	return s.data, nil
}
