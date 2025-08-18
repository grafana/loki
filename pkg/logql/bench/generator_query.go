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

var DefaultTestCaseGeneratorConfig = TestCaseGeneratorConfig{
	RangeType:     "range",
	RangeInterval: time.Hour,
}

type TestCaseGeneratorConfig struct {
	RangeType     string        // "instant" or "range"
	RangeInterval time.Duration // Interval for range queries, e.g. "5m", "1h"
}

type TestCaseGenerator struct {
	logGenCfg *GeneratorConfig
	cfg       TestCaseGeneratorConfig
}

func NewTestCaseGenerator(cfg TestCaseGeneratorConfig, logGenCfg *GeneratorConfig) *TestCaseGenerator {
	return &TestCaseGenerator{
		cfg:       cfg,
		logGenCfg: logGenCfg,
	}
}

// GenerateTestCases creates a sorted list of test cases using the configuration
func (g *TestCaseGenerator) Generate() []TestCase {
	var cases []TestCase
	labelCombos := g.logGenCfg.generateLabelCombinations()

	start := g.logGenCfg.StartTime
	end := g.logGenCfg.StartTime.Add(g.logGenCfg.TimeSpread)
	rangeInterval := g.cfg.RangeInterval
	// Calculate step size to get ~20 points over the time range
	step := g.logGenCfg.TimeSpread / 19

	if g.cfg.RangeType == "instant" {
		// for instant queries, search the whole time spread from end
		start = end
		step = 0
	}

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

	// Basic label selector queries with line filters and structured metadata
	if g.cfg.RangeType != "instant" { // log queries only support range type
		for _, combo := range labelCombos {
			selector := g.logGenCfg.buildLabelSelector(combo)

			// Basic selector
			addBidirectional(selector, g.logGenCfg.StartTime, end)

			// With line filter
			addBidirectional(selector+` |= "level"`, g.logGenCfg.StartTime, end)

			// With structured metadata filters
			addBidirectional(selector+` | detected_level="error"`, g.logGenCfg.StartTime, end)
			addBidirectional(selector+` | detected_level="warn"`, g.logGenCfg.StartTime, end)

			// Combined filters
			addBidirectional(selector+` |~ "error|exception" | detected_level="error"`, g.logGenCfg.StartTime, end)
			addBidirectional(selector+` | json | duration_seconds > 0.1 | detected_level!="debug"`, g.logGenCfg.StartTime, end)
			addBidirectional(selector+` | logfmt | level="error" | detected_level="error"`, g.logGenCfg.StartTime, end)
		}
	}

	for _, combo := range labelCombos {
		selector := g.logGenCfg.buildLabelSelector(combo)

		// Metric queries with structured metadata
		baseRangeAggregationQueries := []string{
			fmt.Sprintf(`count_over_time(%s[%s])`, selector, rangeInterval),
			fmt.Sprintf(`count_over_time(%s | detected_level=~"error|warn" [%s])`, selector, rangeInterval),
			fmt.Sprintf(`count_over_time(%s |= "level" [%s])`, selector, rangeInterval),
			fmt.Sprintf(`rate(%s | detected_level=~"error|warn" [%s])`, selector, rangeInterval),
		}

		// Single dimension aggregations
		dimensions := []string{"pod", "namespace", "env", "detected_level"}
		for _, dim := range dimensions {
			for _, baseMetricQuery := range baseRangeAggregationQueries {
				query := fmt.Sprintf(`sum by (%s) (%s)`, dim, baseMetricQuery)
				addMetricQuery(query, start, end, step)
			}
		}

		// Two dimension aggregations
		twoDimCombos := [][]string{
			{"cluster", "namespace"},
			{"env", "component"},
		}
		for _, dims := range twoDimCombos {
			for _, baseMetricQuery := range baseRangeAggregationQueries {
				query := fmt.Sprintf(`sum by (%s, %s) (%s)`, dims[0], dims[1], baseMetricQuery)
				addMetricQuery(query, start, end, step)
			}
		}
	}

	// Dense period queries
	for _, interval := range g.logGenCfg.DenseIntervals {
		combo := labelCombos[g.logGenCfg.NewRand().Intn(len(labelCombos))]
		selector := g.logGenCfg.buildLabelSelector(combo)
		if g.cfg.RangeType != "instant" {
			addBidirectional(
				selector+` | detected_level=~"error|warn"`,
				interval.Start,
				interval.Start.Add(interval.Duration),
			)
		}

		start := interval.Start
		end := interval.Start.Add(interval.Duration)
		step := interval.Duration / 19
		rangeInterval := time.Minute
		if g.cfg.RangeType == "instant" {
			start = end
			step = 0
			rangeInterval = g.cfg.RangeInterval
		}

		// Add metric queries for dense periods
		rateQuery := fmt.Sprintf(`sum by (component, detected_level) (rate(%s[%s]))`, selector, rangeInterval)
		addMetricQuery(
			rateQuery,
			start,
			end,
			step,
		)
	}

	// Sort test cases by name for consistent ordering
	sort.Slice(cases, func(i, j int) bool {
		return cases[i].Name() < cases[j].Name()
	})

	return cases
}
