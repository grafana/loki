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
	summary := parse(os.Stdin)
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
	pkg    string
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

	testsByPackage := make(map[string][]string, 0)
	for _, r := range s.results {
		if r.status == Fail {
			if _, ok := testsByPackage[r.pkg]; !ok {
				testsByPackage[r.pkg] = []string{r.test}
			} else {
				testsByPackage[r.pkg] = append(testsByPackage[r.pkg], r.test)
			}
		}
	}

	if len(testsByPackage) > 0 {
		sw.WriteString("## Failed Tests\n")
		sw.WriteString(`| Package | Test |
| --- | --- |
`)
	}

	for pkg, tests := range testsByPackage {
		// Only print package name once.
		sw.WriteString("| ")
		sw.WriteString(pkg)
		sw.WriteString(" | ")
		sw.WriteString(tests[0])
		sw.WriteString(" |\n")
		for _, test := range tests[1:] {
			sw.WriteString("| | ")
			sw.WriteString(test)
			sw.WriteString(" |\n")
		}
	}

	sw.Flush()
}

type TokenType int

const (
	trippleEquals TokenType = iota
	result
	test
)

type Token struct {
	token TokenType
	text  string
}

func parse(r io.Reader) *TestSummary {
	tokens := lex(r)

	summary := &TestSummary{}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		next := peek(tokens, i)
		if token.token == result && next.token == test {
			i++
			status, _ := parseStatus(token.text)
			switch status {
			case Pass:
				summary.Add(TestResult{status: status, test: next.text})
			case Skip, Fail:
				// Skip and Fail have the pattern: <Status> <package> <test name>
				testName := peek(tokens, i)
				i++
				summary.Add(TestResult{
					status: status,
					test:   testName.text,
					pkg:    next.text,
				})
			}
		}
	}

	return summary
}

func lex(r io.Reader) []Token {
	var s scanner.Scanner
	s.Init(os.Stdin)
	s.Mode = scanner.GoTokens
	tokens := make([]Token, 0)
	b, _ := io.ReadAll(r)
	fields := strings.Fields(string(b))
	for i := 0; i < len(fields); i++ {
		text := fields[i]
		switch text {
		case "===":
			tokens = append(tokens, Token{token: trippleEquals})
		case "PASS":
			tokens = append(tokens, Token{token: result, text: text})
		case "FAIL:", "SKIP:":
			tokens = append(tokens, Token{token: result, text: strings.TrimSuffix(text, ":")})
		default:
			tokens = append(tokens, Token{token: test, text: text})
		}
	}

	return tokens
}

func peek(tokens []Token, i int) Token {
	next := i + 1
	if next >= len(tokens) {
		return Token{}
	}

	return tokens[next]
}

func parseStatus(s string) (Status, error) {
	switch s {
	case "PASS":
		return Pass, nil
	case "FAIL":
		return Fail, nil
	case "SKIP":
		return Skip, nil
	default:
		return Status(""), fmt.Errorf("unknown test status: %s", s)
	}
}
