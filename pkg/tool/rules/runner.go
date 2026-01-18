package rules

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

// testRunner executes unit tests and collects results.
type testRunner struct {
	evaluator *testEvaluator
	assertion *testAssertion
	logger    log.Logger
}

// newTestRunner creates a new test runner.
func newTestRunner(evaluator *testEvaluator, logger log.Logger) *testRunner {
	return &testRunner{
		evaluator: evaluator,
		assertion: newTestAssertion(),
		logger:    logger,
	}
}

// runTests executes all tests in a test file and returns results.
func (tr *testRunner) runTests(testFile *unitTestFile, ruleGroups []*ruleGroup, groupOrderMap map[string]int) (*testResults, error) {
	results := &testResults{
		TestFile:   testFile,
		TestGroups: make([]*testGroupResult, 0),
		StartTime:  time.Now(),
	}

	// Run each test group
	for i := range testFile.Tests {
		groupResult := tr.runTestGroup(&testFile.Tests[i], ruleGroups, groupOrderMap)
		results.TestGroups = append(results.TestGroups, groupResult)

		// Update overall stats
		results.TotalTests += groupResult.TotalTests
		results.PassedTests += groupResult.PassedTests
		results.FailedTests += groupResult.FailedTests
	}

	results.EndTime = time.Now()
	results.Duration = results.EndTime.Sub(results.StartTime)

	return results, nil
}

// runTestGroup executes a single test group.
func (tr *testRunner) runTestGroup(testGroup *testGroup, ruleGroups []*ruleGroup, groupOrderMap map[string]int) *testGroupResult {
	result := &testGroupResult{
		TestGroup:  testGroup,
		TestCases:  make([]*testCaseResult, 0),
		StartTime:  time.Now(),
		TotalTests: len(testGroup.AlertRuleTests) + len(testGroup.LogQLExprTests),
	}

	// Create fresh storage and load input streams
	storage := newTestStorage()
	err := storage.parseAndLoadStreams(testGroup.InputStreams, testGroup.Interval)
	if err != nil {
		result.Error = fmt.Errorf("failed to load input streams: %w", err)
		result.FailedTests = result.TotalTests
		return result
	}

	// Create new evaluator with the loaded data
	evaluator := newTestEvaluator(storage, tr.logger)

	// Copy rule groups to the new evaluator
	evaluator.ruleGroups = make([]*ruleGroup, len(ruleGroups))
	copy(evaluator.ruleGroups, ruleGroups)

	// Sort if needed
	if len(groupOrderMap) > 0 {
		evaluator.sortRuleGroups(groupOrderMap)
	}

	// Replace the evaluator for this test group
	oldEvaluator := tr.evaluator
	tr.evaluator = evaluator
	defer func() { tr.evaluator = oldEvaluator }()

	// Run alert rule tests
	for i := range testGroup.AlertRuleTests {
		testCase := tr.runAlertTest(&testGroup.AlertRuleTests[i], testGroup)
		result.TestCases = append(result.TestCases, testCase)
		if testCase.Passed {
			result.PassedTests++
		} else {
			result.FailedTests++
		}
	}

	// Run LogQL expression tests
	for i := range testGroup.LogQLExprTests {
		testCase := tr.runLogQLTest(&testGroup.LogQLExprTests[i], testGroup)
		result.TestCases = append(result.TestCases, testCase)
		if testCase.Passed {
			result.PassedTests++
		} else {
			result.FailedTests++
		}
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result
}

// runAlertTest executes a single alert rule test.
func (tr *testRunner) runAlertTest(alertTest *alertTestCase, testGroup *testGroup) *testCaseResult {
	result := &testCaseResult{
		Name:      fmt.Sprintf("alert: %s at %s", alertTest.Alertname, alertTest.EvalTime),
		StartTime: time.Now(),
	}

	// Evaluate rules at the specified time
	evalTime := time.Unix(0, 0).UTC().Add(time.Duration(alertTest.EvalTime))
	ctx := user.InjectOrgID(context.Background(), "test-tenant")

	err := tr.evaluator.evaluateAtTime(ctx, evalTime)
	if err != nil {
		result.Error = fmt.Errorf("evaluation failed: %w", err)
		result.Passed = false
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result
	}

	// Get active alerts for this rule
	actualAlerts := tr.evaluator.getActiveAlertsForRule(alertTest.Alertname)

	// Compare expected vs actual
	err = tr.assertion.compareAlerts(*alertTest, actualAlerts)
	if err != nil {
		result.Error = err
		result.Passed = false
	} else {
		result.Passed = true
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result
}

// runLogQLTest executes a single LogQL expression test.
func (tr *testRunner) runLogQLTest(logqlTest *logqlTestCase, testGroup *testGroup) *testCaseResult {
	result := &testCaseResult{
		Name:      fmt.Sprintf("logql: %s at %s", logqlTest.Expr, logqlTest.EvalTime),
		StartTime: time.Now(),
	}

	// Parse expression to determine if it's a log or metric query
	expr, err := syntax.ParseExpr(logqlTest.Expr)
	if err != nil {
		result.Error = fmt.Errorf("failed to parse expression: %w", err)
		result.Passed = false
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result
	}

	// Execute the query
	evalTime := time.Unix(0, 0).UTC().Add(time.Duration(logqlTest.EvalTime))
	ctx := user.InjectOrgID(context.Background(), "test-tenant")

	queryResult, err := tr.evaluator.executeLogQLQuery(ctx, logqlTest.Expr, evalTime)
	if err != nil {
		result.Error = fmt.Errorf("query execution failed: %w", err)
		result.Passed = false
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result
	}

	// Compare results based on query type
	isLogQuery := IsLogQuery(expr)
	if isLogQuery {
		err = tr.assertion.compareLogQLLogs(*logqlTest, queryResult)
	} else {
		err = tr.assertion.compareLogQLSamples(*logqlTest, queryResult)
	}

	if err != nil {
		result.Error = err
		result.Passed = false
	} else {
		result.Passed = true
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result
}

// testResults holds the results of running all tests.
type testResults struct {
	TestFile    *unitTestFile
	TestGroups  []*testGroupResult
	TotalTests  int
	PassedTests int
	FailedTests int
	StartTime   time.Time
	EndTime     time.Time
	Duration    time.Duration
}

// testGroupResult holds the results of running a test group.
type testGroupResult struct {
	TestGroup   *testGroup
	TestCases   []*testCaseResult
	TotalTests  int
	PassedTests int
	FailedTests int
	StartTime   time.Time
	EndTime     time.Time
	Duration    time.Duration
	Error       error
}

// testCaseResult holds the result of a single test case.
type testCaseResult struct {
	Name      string
	Passed    bool
	Error     error
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
}

// executeLogQLQuery is a helper that executes a LogQL query and returns the result.
func (te *testEvaluator) executeLogQLQuery(ctx context.Context, expr string, evalTime time.Time) (*logqlmodel.Result, error) {
	// Parse expression
	parsedExpr, err := syntax.ParseExpr(expr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse expression: %w", err)
	}

	// Create query parameters - use evalTime for both start and end (instant query)
	params, err := logql.NewLiteralParams(
		expr,
		evalTime,
		evalTime,
		0, // step (instant query)
		0, // interval
		logproto.BACKWARD,
		0,   // limit
		nil, // shards
		nil, // storeChunks
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create params: %w", err)
	}

	// Execute query
	query := te.engine.Query(params)
	result, err := query.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}

	_ = parsedExpr // parsed expr used for type checking elsewhere

	return &result, nil
}

// RunUnitTests is the main entry point for running unit tests.
func RunUnitTests(testFiles []string, logger log.Logger) error {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	allResults := make([]*testResults, 0)
	totalPassed := 0
	totalFailed := 0

	// Parse and run each test file
	for _, testFile := range testFiles {
		// Parse test file
		unitTest, err := parseTestFile(testFile)
		if err != nil {
			return fmt.Errorf("failed to parse test file %s: %w", testFile, err)
		}

		// Validate
		if err := unitTest.validate(); err != nil {
			return fmt.Errorf("test file %s validation failed: %w", testFile, err)
		}

		// Create storage and evaluator
		storage := newTestStorage()
		evaluator := newTestEvaluator(storage, logger)

		// Load rule files
		err = evaluator.loadRules(
			unitTest.RuleFiles,
			unitTest.EvaluationInterval,
			labels.EmptyLabels(),
			"",
		)
		if err != nil {
			return fmt.Errorf("failed to load rule files: %w", err)
		}

		// Prepare group order map
		groupOrderMap := make(map[string]int)
		if len(unitTest.GroupEvalOrder) > 0 {
			for i, name := range unitTest.GroupEvalOrder {
				groupOrderMap[name] = i
			}
		}

		// Create and run test runner
		runner := newTestRunner(evaluator, logger)
		results, err := runner.runTests(unitTest, evaluator.ruleGroups, groupOrderMap)
		if err != nil {
			return fmt.Errorf("failed to run tests in %s: %w", testFile, err)
		}

		allResults = append(allResults, results)
		totalPassed += results.PassedTests
		totalFailed += results.FailedTests
	}

	// Print summary
	printTestResults(allResults)

	// Return error if any tests failed
	if totalFailed > 0 {
		return fmt.Errorf("%d of %d tests failed", totalFailed, totalPassed+totalFailed)
	}

	return nil
}

// printTestResults prints a summary of test results to stdout in promtool style.
func printTestResults(allResults []*testResults) {
	// Count total failures
	totalFailed := 0
	for _, results := range allResults {
		totalFailed += results.FailedTests
	}

	// If there are failures, print them in promtool style
	if totalFailed > 0 {
		fmt.Println("FAILED:")
		for _, results := range allResults {
			for _, groupResult := range results.TestGroups {
				if groupResult.Error != nil {
					fmt.Printf("  name: %s,\n", groupResult.TestGroup.TestGroupName)
					fmt.Printf("    Error: %v\n\n", groupResult.Error)
					continue
				}

				for _, testCase := range groupResult.TestCases {
					if !testCase.Passed && testCase.Error != nil {
						fmt.Printf("  name: %s,%v\n\n", groupResult.TestGroup.TestGroupName, testCase.Error)
					}
				}
			}
		}
		return
	}

	// If all tests passed, print simple success message
	fmt.Println("SUCCESS")
}
