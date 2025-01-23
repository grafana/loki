// Import the array of test suites from _exports.libsonnet
local testCases = import './tests/_exports.libsonnet';

// Helper function to process individual test cases
// Compares expected vs actual values and returns a structured result
local runTestCase(testCase) = {
  name: testCase.name,
  success: testCase.expected == testCase.test,
  expected: testCase.expected,
  actual: testCase.test,
};

// Helper function to process a group of test cases
local runTestGroup(group) = {
  name: group.name,
  results: std.map(runTestCase, group.cases),
  summary: {
    total: std.length(group.cases),
    passed: std.length(std.filter(function(r) r.success, std.map(runTestCase, group.cases))),
  },
};

// Process each test suite in the imported array
local suites = [
  {
    name: suite.name,
    // Now we can directly map over the tests array
    groups: std.map(runTestGroup, suite.tests),
    summary: {
      total: std.foldl(function(sum, group) sum + group.summary.total, std.map(runTestGroup, suite.tests), 0),
      passed: std.foldl(function(sum, group) sum + group.summary.passed, std.map(runTestGroup, suite.tests), 0),
    },
  }
  for suite in testCases
];

// Output final test results structure
{
  suites: suites,
  summary: {
    total: std.foldl(function(sum, suite) sum + suite.summary.total, suites, 0),
    passed: std.foldl(function(sum, suite) sum + suite.summary.passed, suites, 0),
  },
}
