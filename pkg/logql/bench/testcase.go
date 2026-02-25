package bench

import (
	"fmt"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// TestCase represents a LogQL test case for benchmarking and testing
type TestCase struct {
	Query     string
	Start     time.Time
	End       time.Time
	Direction logproto.Direction
	Step      time.Duration // Step size for metric queries
	Source    string        // Source location (suite/file.yaml:line)
	QueryDesc string        // Query description from YAML
}

// Name returns a descriptive name for the test case.
// For log queries, it includes the direction.
// For metric queries (rate, sum), it returns the query with step size.
func (c TestCase) Name() string {
	expr, err := syntax.ParseExpr(c.Query)
	if err != nil {
		return fmt.Sprintf("%s [%v]", c.Query, c.Direction)
	}
	if _, ok := expr.(syntax.SampleExpr); ok {
		return c.Query
	}
	return fmt.Sprintf("%s [%v]", c.Query, c.Direction)
}

// Kind returns the kind of the test case based on the query type.
func (c TestCase) Kind() string {
	expr, err := syntax.ParseExpr(c.Query)
	if err != nil {
		return "invalid"
	}
	if _, ok := expr.(syntax.SampleExpr); ok {
		return "metric"
	}
	return "log"
}

// Description returns a detailed description of the test case including time range
func (c TestCase) Description() string {
	var b strings.Builder
	if c.Source != "" {
		b.WriteString(fmt.Sprintf("Source: %s\n", c.Source))
	}
	b.WriteString(fmt.Sprintf("Query: %s\n", c.Query))
	b.WriteString(fmt.Sprintf("Time Range: %v to %v\n", c.Start.Format(time.RFC3339), c.End.Format(time.RFC3339)))
	if c.Step > 0 {
		b.WriteString(fmt.Sprintf("Step: %v\n", c.Step))
	}
	b.WriteString(fmt.Sprintf("Direction: %v", c.Direction))
	return b.String()
}
