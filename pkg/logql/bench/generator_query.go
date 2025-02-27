package bench

import (
	"fmt"
	"sort"
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

// Description returns a detailed description of the test case including time range
func (c TestCase) Description() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Query: %s\n", c.Query))
	b.WriteString(fmt.Sprintf("Time Range: %v to %v\n", c.Start.Format(time.RFC3339), c.End.Format(time.RFC3339)))
	if c.Step > 0 {
		b.WriteString(fmt.Sprintf("Step: %v\n", c.Step))
	}
	b.WriteString(fmt.Sprintf("Direction: %v", c.Direction))
	return b.String()
}

type labelMatcher struct {
	name, value string
}

func (c *GeneratorConfig) buildLabelSelector(matchers []labelMatcher) string {
	var parts []string
	for _, m := range matchers {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, m.name, m.value))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func (c *GeneratorConfig) generateLabelCombinations() [][]labelMatcher {
	rnd := c.NewRand()

	// Helper to create a matcher with random value
	randomMatcher := func(name string, values []string) labelMatcher {
		return labelMatcher{name: name, value: values[rnd.Intn(len(values))]}
	}
	components := []string{}
	for _, app := range defaultApplications {
		components = append(components, app.Name)
	}
	combinations := [][]labelMatcher{
		// Single label matchers
		{randomMatcher("service_name", components)},
		{randomMatcher("region", c.LabelConfig.Regions)},

		// Two label combinations
		{
			randomMatcher("region", c.LabelConfig.Regions),
			randomMatcher("env", c.LabelConfig.EnvTypes),
		},

		// Three label combinations
		{
			randomMatcher("service_name", components),
			randomMatcher("env", c.LabelConfig.EnvTypes),
			randomMatcher("region", c.LabelConfig.Regions),
		},
	}

	return combinations
}

// GenerateTestCases creates a sorted list of test cases using the configuration
func (c *GeneratorConfig) GenerateTestCases() []TestCase {
	var cases []TestCase
	labelCombos := c.generateLabelCombinations()
	end := c.StartTime.Add(c.TimeSpread)

	// Use a map to track unique queries
	uniqueQueries := make(map[string]struct{})

	// Helper to add both forward and backward variants if query is unique
	addBidirectional := func(query string, start, end time.Time) {
		if _, exists := uniqueQueries[query]; exists {
			return // Skip duplicate queries
		}
		uniqueQueries[query] = struct{}{}
		cases = append(cases,
			TestCase{
				Query:     query,
				Start:     start,
				End:       end,
				Direction: logproto.FORWARD,
			},
			TestCase{
				Query:     query,
				Start:     start,
				End:       end,
				Direction: logproto.BACKWARD,
			},
		)
	}

	// Helper to add metric query if unique
	addMetricQuery := func(query string, start, end time.Time, step time.Duration) {
		if _, exists := uniqueQueries[query]; exists {
			return // Skip duplicate queries
		}
		uniqueQueries[query] = struct{}{}
		cases = append(cases,
			TestCase{
				Query:     query,
				Start:     start,
				End:       end,
				Direction: logproto.FORWARD,
				Step:      step,
			},
		)
	}

	// Calculate step size to get ~20 points over the time range
	step := c.TimeSpread / 19

	// Basic label selector queries with line filters and structured metadata
	for _, combo := range labelCombos {
		selector := c.buildLabelSelector(combo)

		// Basic selector
		addBidirectional(selector, c.StartTime, end)

		// With structured metadata filters
		addBidirectional(selector+` | detected_level="error"`, c.StartTime, end)
		addBidirectional(selector+` | detected_level="warn"`, c.StartTime, end)

		// Combined filters
		addBidirectional(selector+` |~ "error|exception" | detected_level="error"`, c.StartTime, end)
		addBidirectional(selector+` | json | duration_seconds > 0.1 | detected_level!="debug"`, c.StartTime, end)
		addBidirectional(selector+` | logfmt | level="error" | detected_level="error"`, c.StartTime, end)

		// Metric queries with structured metadata
		baseMetricQuery := fmt.Sprintf(`rate(%s | detected_level=~"error|warn" [5m])`, selector)

		// Single dimension aggregations
		dimensions := []string{"pod", "namespace", "env"}
		for _, dim := range dimensions {
			query := fmt.Sprintf(`sum by (%s) (%s)`, dim, baseMetricQuery)
			addMetricQuery(query, c.StartTime.Add(5*time.Minute), end, step)
		}

		// Two dimension aggregations
		twoDimCombos := [][]string{
			{"cluster", "namespace"},
			{"env", "component"},
		}
		for _, dims := range twoDimCombos {
			query := fmt.Sprintf(`sum by (%s, %s) (%s)`, dims[0], dims[1], baseMetricQuery)
			addMetricQuery(query, c.StartTime.Add(5*time.Minute), end, step)
		}

		// Error rates by severity
		errorQueries := []string{
			fmt.Sprintf(`sum by (component) (rate(%s | detected_level="error" [5m]))`, selector),
			fmt.Sprintf(`sum by (component) (rate(%s | detected_level="warn" [5m]))`, selector),
			fmt.Sprintf(`sum by (component, detected_level) (rate(%s | detected_level=~"error|warn" [5m]))`, selector),
		}
		for _, query := range errorQueries {
			addMetricQuery(query, c.StartTime.Add(5*time.Minute), end, step)
		}
	}

	// Dense period queries
	for _, interval := range c.DenseIntervals {
		combo := labelCombos[c.NewRand().Intn(len(labelCombos))]
		selector := c.buildLabelSelector(combo)
		addBidirectional(
			selector+` | detected_level=~"error|warn"`,
			interval.Start,
			interval.Start.Add(interval.Duration),
		)

		// Add metric queries for dense periods
		rateQuery := fmt.Sprintf(`sum by (component, detected_level) (rate(%s[1m]))`, selector)
		addMetricQuery(
			rateQuery,
			interval.Start,
			interval.Start.Add(interval.Duration),
			interval.Duration/19,
		)
	}

	// Sort test cases by name for consistent ordering
	sort.Slice(cases, func(i, j int) bool {
		return cases[i].Name() < cases[j].Name()
	})

	return cases
}
