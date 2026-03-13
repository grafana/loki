package main

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	junit "github.com/joshdk/go-junit"
)

const testPrefix = "TestRemoteStorageEquality/"

// testLabels holds the labels extracted from a JUnit test case name.
type testLabels struct {
	Suite     string
	QueryFile string
	Kind      string
	Direction string
}

// parseTestName extracts structured labels from a JUnit test case name.
// Expected format: TestRemoteStorageEquality/{suite}/{file}.yaml:{line}/kind={kind}/direction={direction}
func parseTestName(name string) (*testLabels, error) {
	if !strings.HasPrefix(name, testPrefix) {
		return nil, fmt.Errorf("missing prefix %q", testPrefix)
	}
	rest := strings.TrimPrefix(name, testPrefix)

	segments := strings.Split(rest, "/")
	if len(segments) != 4 {
		return nil, fmt.Errorf("expected 4 segments, got %d: %q", len(segments), rest)
	}

	suite := segments[0]

	// Parse query_file: "basic.yaml:3" → split on ":" → "basic.yaml" → strip extension
	fileWithLine := segments[1]
	filePart, _, _ := strings.Cut(fileWithLine, ":")
	queryFile := strings.TrimSuffix(strings.TrimSuffix(filePart, ".yaml"), ".yml")

	// Parse kind: "kind=metric" → "metric"
	kindSeg := segments[2]
	if !strings.HasPrefix(kindSeg, "kind=") {
		return nil, fmt.Errorf("segment %q missing 'kind=' prefix", kindSeg)
	}
	kind := strings.TrimPrefix(kindSeg, "kind=")

	// Parse direction: "direction=FORWARD" → "FORWARD"
	dirSeg := segments[3]
	if !strings.HasPrefix(dirSeg, "direction=") {
		return nil, fmt.Errorf("segment %q missing 'direction=' prefix", dirSeg)
	}
	direction := strings.TrimPrefix(dirSeg, "direction=")

	return &testLabels{
		Suite:     suite,
		QueryFile: queryFile,
		Kind:      kind,
		Direction: direction,
	}, nil
}

type testStatus string

const (
	statusPass  testStatus = "pass"
	statusFail  testStatus = "fail"
	statusError testStatus = "error"
	statusSkip  testStatus = "skip"
)

// parsedTest holds a single test result with optional parsed labels.
type parsedTest struct {
	Name        string
	Labels      *testLabels // nil if name is unparseable
	Status      testStatus
	DurationSec float64
}

// parsedResults holds all parsed test results and run-level data.
type parsedResults struct {
	Tests            []parsedTest
	TotalDurationSec float64
}

// xmlSuiteTime is a minimal struct used to extract the time attribute from testsuite XML elements.
type xmlSuiteTime struct {
	Time string `xml:"time,attr"`
}

// extractSuiteDurationSec parses the XML data and sums the time attributes from all <testsuite> elements.
// We parse the raw XML directly instead of using go-junit's Totals.Duration because the library
// recalculates duration by summing individual test case durations (via Aggregate()), which
// overwrites the original wall-clock time from the XML time= attribute. For parallel test
// execution, wall-clock time < sum of test durations, so the XML attribute is more accurate.
func extractSuiteDurationSec(data []byte) float64 {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	var total float64
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "testsuite" {
			continue
		}
		var s xmlSuiteTime
		_ = dec.DecodeElement(&s, &se)
		if s.Time != "" {
			if v, err := strconv.ParseFloat(s.Time, 64); err == nil {
				total += v
			}
		}
	}
	return total
}

// parseJUnitXML parses JUnit XML bytes into structured test results.
func parseJUnitXML(data []byte) (*parsedResults, error) {
	suites, err := junit.Ingest(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JUnit XML: %w", err)
	}

	var results parsedResults

	for _, suite := range suites {
		for _, tc := range suite.Tests {
			pt := parsedTest{
				Name:        tc.Name,
				DurationSec: tc.Duration.Seconds(),
			}

			switch tc.Status {
			case junit.StatusPassed:
				pt.Status = statusPass
			case junit.StatusFailed:
				pt.Status = statusFail
			case junit.StatusError:
				pt.Status = statusError
			case junit.StatusSkipped:
				pt.Status = statusSkip
			default:
				pt.Status = statusPass
			}

			labels, parseErr := parseTestName(tc.Name)
			if parseErr == nil {
				pt.Labels = labels
			}

			results.Tests = append(results.Tests, pt)
		}
	}

	// Use the suite-level time attribute from the XML as the total duration.
	results.TotalDurationSec = extractSuiteDurationSec(data)

	// Fallback: if suite duration was zero, sum test case durations
	if results.TotalDurationSec == 0 {
		for _, t := range results.Tests {
			results.TotalDurationSec += t.DurationSec
		}
	}

	return &results, nil
}
