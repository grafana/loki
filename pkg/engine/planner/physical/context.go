package physical

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	binOpToMatchTypeMapping = map[types.BinaryOp]labels.MatchType{
		types.BinaryOpEq:         labels.MatchEqual,
		types.BinaryOpNeq:        labels.MatchNotEqual,
		types.BinaryOpMatchRe:    labels.MatchRegexp,
		types.BinaryOpNotMatchRe: labels.MatchNotRegexp,
	}
)

// Catalog is an interface that provides methods for interacting with
// storage metadata. In traditional database systems there are system tables
// providing this information (e.g. pg_catalog, ...) whereas in Loki there
// is the Metastore.
type Catalog interface {
	ResolveDataObj(Expression, time.Duration) ([]DataObjLocation, [][]int64, error)
}

// Context is the default implementation of [Catalog].
type Context struct {
	ctx           context.Context
	metastore     metastore.Metastore
	from, through time.Time
}

// NewContext creates a new instance of [Context] for query planning.
func NewContext(ctx context.Context, ms metastore.Metastore, from, through time.Time) *Context {
	return &Context{
		ctx:       ctx,
		metastore: ms,
		from:      from,
		through:   through,
	}
}

// ResolveDataObj resolves DataObj locations and streams IDs based on a given
// [Expression]. The expression is required to be a (tree of) [BinaryExpression]
// with a [ColumnExpression] on the left and a [LiteralExpression] on the right.
func (c *Context) ResolveDataObj(selector Expression, rangeInterval time.Duration) ([]DataObjLocation, [][]int64, error) {
	if c.metastore == nil {
		return nil, nil, errors.New("no metastore to resolve objects")
	}

	matchers, err := expressionToMatchers(selector)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert selector expression into matchers: %w", err)
	}

	// extend search by rangeInterval to be able to include entries belonging to the [$range] interval.
	paths, streamIDs, err := c.metastore.StreamIDs(c.ctx, c.from.Add(-rangeInterval), c.through, matchers...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve data object locations: %w", err)
	}

	locations := make([]DataObjLocation, 0, len(paths))
	for _, loc := range paths {
		locations = append(locations, DataObjLocation(loc))
	}
	return locations, streamIDs, err
}

func expressionToMatchers(selector Expression) ([]*labels.Matcher, error) {
	if selector == nil {
		return nil, nil
	}

	switch expr := selector.(type) {
	case *BinaryExpr:
		switch expr.Op {
		case types.BinaryOpAnd:
			lhs, err := expressionToMatchers(expr.Left)
			if err != nil {
				return nil, err
			}
			rhs, err := expressionToMatchers(expr.Right)
			if err != nil {
				return nil, err
			}
			return append(lhs, rhs...), nil
		case types.BinaryOpEq, types.BinaryOpNeq, types.BinaryOpMatchRe, types.BinaryOpNotMatchRe:
			op, err := convertBinaryOp(expr.Op)
			if err != nil {
				return nil, err
			}
			name, err := convertColumnRef(expr.Left)
			if err != nil {
				return nil, err
			}
			value, err := convertLiteralToString(expr.Right)
			if err != nil {
				return nil, err
			}
			lhs, err := labels.NewMatcher(op, name, value)
			if err != nil {
				return nil, err
			}
			return []*labels.Matcher{lhs}, nil
		default:
			return nil, fmt.Errorf("invalid binary expression in stream selector expression: %v", expr.Op.String())
		}
	default:
		return nil, fmt.Errorf("invalid expression type in stream selector expression: %T", expr)
	}
}

func convertLiteralToString(expr Expression) (string, error) {
	l, ok := expr.(*LiteralExpr)
	if !ok {
		return "", fmt.Errorf("expected literal expression, got %T", expr)
	}
	if l.ValueType() != datatype.String {
		return "", fmt.Errorf("literal type is not a string, got %v", l.ValueType())
	}
	return l.Any().(string), nil
}

func convertColumnRef(expr Expression) (string, error) {
	ref, ok := expr.(*ColumnExpr)
	if !ok {
		return "", fmt.Errorf("expected column expression, got %T", expr)
	}
	if ref.Ref.Type != types.ColumnTypeLabel {
		return "", fmt.Errorf("column type is not a label, got %v", ref.Ref.Type)
	}
	return ref.Ref.Column, nil
}

func convertBinaryOp(t types.BinaryOp) (labels.MatchType, error) {
	ty, ok := binOpToMatchTypeMapping[t]
	if !ok {
		return -1, fmt.Errorf("invalid binary operator for matcher: %v", t)
	}
	return ty, nil
}

var _ Catalog = (*Context)(nil)
