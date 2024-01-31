package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
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
			sw.WriteString("\n")
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
			status, _ := parseStatus(token.text)
			summary.Add(TestResult{status: status, test: next.text})
			i++
		}
	}

	return summary
}

func lex(r io.Reader) []Token {
	var s scanner.Scanner
	s.Init(os.Stdin)
	s.Mode = scanner.GoTokens
	tokens := make([]Token, 0)
	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		switch text := s.TokenText(); {
		case text == "===":
			tokens = append(tokens, Token{token: trippleEquals})
		case text == "PASS":
			tokens = append(tokens, Token{token: result, text: text})
		case text == "FAIL" || text == "SKIP":
			// both are followed by ':'
			if s.Peek() == ':' {
				tokens = append(tokens, Token{token: result, text: text})
			}
			// Skip ':'
			s.Scan()
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
