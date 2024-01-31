package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"text/scanner"
)

func main() {

	summary := &TestSummary{}

	var s scanner.Scanner
	s.Init(os.Stdin)
	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
	}
	for reader.Scan() {
		result, err := parse(reader.Text())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warn: %s\n", err)
			continue
		}
		summary.Add(result)

	}

	summary.Write(os.Stdout)
}

type Status string

const (
	Pass Status = "Pass"
	Fail Status = "Fail"
	Skip Status = "Skip"
)

type TestResult struct {
	status Status
	test   string
}

type TestSummary struct {
	results []TestResult
}

func (s *TestSummary) Add(r TestResult) {
	s.results = append(s.results, r)
}

func (s *TestSummary) Write(w io.Writer) {
	sw := bufio.NewWriter(w)
	sw.WriteString("# Test Summary\n")

	passedTests := 0
	failedTests := 0
	skippedTets := 0
	for _, r := range s.results {
		switch r.status {
		case Pass:
			passedTests++
		case Fail:
			failedTests++
		case Skip:
			skippedTets++
		}
	}
	sw.WriteString(fmt.Sprintf("%d ✅, %d ❌\n", passedTests, failedTests))

	sw.WriteString("## Failed Tests\n")
	for _, r := range s.results {
		if r.status == Fail {
			sw.WriteString(r.test)
		}
	}

	sw.Flush()
}

func parse(line string) (TestResult, error) {

	fields := strings.Fields(line)
	if len(fields) < 2 {
		return TestResult{}, fmt.Errorf("too few test result fields: %d", len(fields))
	}

	status, err := parseStatus(fields[0])
	if err != nil {
		return TestResult{}, fmt.Errorf("error parsing status: %w", err)
	}

	return TestResult{
		status: status,
		test:   fields[1],
	}, nil
}

func parseStatus(s string) (Status, error) {
	switch s {
	case "PASS":
		return Pass, nil
	case "FAIL":
		return Fail, nil
	case "SKIPPED":
		return Skip, nil
	default:
		return Status(""), fmt.Errorf("unknown test status: %s", s)
	}
}
