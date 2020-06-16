package astmapper

import (
	"encoding/json"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

/*
Design:

The prometheus api package enforces a (*promql.Engine argument), making it infeasible to do lazy AST
evaluation and substitution from within this package.
This leaves the (storage.Queryable) interface as the remaining target for conducting application level sharding.

The main idea is to analyze the AST and determine which subtrees can be parallelized. With those in hand, the queries may
be remapped into vector or matrix selectors utilizing a reserved label containing the original query. These may then be parallelized in the storage implementation.
*/

const (
	// QueryLabel is a reserved label containing an embedded query
	QueryLabel = "__cortex_queries__"
	// EmbeddedQueriesMetricName is a reserved label (metric name) denoting an embedded query
	EmbeddedQueriesMetricName = "__embedded_queries__"
)

// EmbeddedQueries is a wrapper type for encoding queries
type EmbeddedQueries struct {
	Concat []string `json:"Concat"`
}

// JSONCodec is a Codec that uses JSON representations of EmbeddedQueries structs
var JSONCodec jsonCodec

type jsonCodec struct{}

func (c jsonCodec) Encode(queries []string) (string, error) {
	embedded := EmbeddedQueries{
		Concat: queries,
	}
	b, err := json.Marshal(embedded)
	return string(b), err
}

func (c jsonCodec) Decode(encoded string) (queries []string, err error) {
	var embedded EmbeddedQueries
	err = json.Unmarshal([]byte(encoded), &embedded)
	if err != nil {
		return nil, err
	}

	return embedded.Concat, nil
}

// VectorSquash reduces an AST into a single vector query which can be hijacked by a Queryable impl.
// It always uses a VectorSelector as the substitution node.
// This is important because logical/set binops can only be applied against vectors and not matrices.
func VectorSquasher(nodes ...parser.Node) (parser.Expr, error) {

	// concat OR legs
	strs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		strs = append(strs, node.String())
	}

	encoded, err := JSONCodec.Encode(strs)
	if err != nil {
		return nil, err
	}

	embeddedQuery, err := labels.NewMatcher(labels.MatchEqual, QueryLabel, encoded)
	if err != nil {
		return nil, err
	}

	return &parser.VectorSelector{
		Name:          EmbeddedQueriesMetricName,
		LabelMatchers: []*labels.Matcher{embeddedQuery},
	}, nil

}
