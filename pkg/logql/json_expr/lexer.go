package json_expr

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"text/scanner"
)

type Scanner struct {
	buf   *bufio.Reader
	data  []interface{}
	err   error
	debug bool
}

func NewScanner(r io.Reader, debug bool) *Scanner {
	return &Scanner{
		buf:   bufio.NewReader(r),
		debug: debug,
	}
}

func (sc *Scanner) Error(s string) {
	fmt.Printf("syntax error: %s\n", s)
}

func (sc *Scanner) Reduced(rule, state int, lval *JSONExprSymType) bool {
	if sc.debug {
		fmt.Printf("rule: %v; state %v; lval: %v\n", rule, state, lval)
	}
	return false
}

func (s *Scanner) Lex(lval *JSONExprSymType) int {
	return s.lex(lval)
}

func (s *Scanner) lex(lval *JSONExprSymType) int {
	for {
		r := s.read()

		if r == 0 {
			return 0
		}
		if isWhitespace(r) {
			continue
		}

		if isDigit(r) {
			s.unread()
			val, err := s.scanInt()
			if err != nil {
				s.err = fmt.Errorf(err.Error())
				return 0
			}

			lval.int = val
			return INDEX
		}

		switch true {
		case r == '[':
			return LSB
		case r == ']':
			return RSB
		case r == '.':
			return DOT
		case isIdentifier(r):
			s.unread()
			lval.field = s.scanField()
			return FIELD
		case r == '"':
			s.unread()
			lval.str = s.scanStr()
			return STRING
		default:
			s.err = fmt.Errorf("unexpected char %c", r)
			return 0
		}
	}
}

func isIdentifier(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
}

func (s *Scanner) scanField() string {
	var str []rune

	for {
		r := s.read()
		if !isIdentifier(r) {
			s.unread()
			break
		}

		if r == '.' || r == scanner.EOF || r == rune(0) {
			s.unread()
			break
		}

		str = append(str, r)
	}
	return string(str)
}

func (s *Scanner) scanStr() string {
	var str []rune
	//begin with ", end with "
	r := s.read()
	if r != '"' {
		s.err = fmt.Errorf("unexpected char %c", r)
		return ""
	}

	for {
		r := s.read()
		if r == '"' || r == 1 || r == ']' {
			break
		}
		str = append(str, r)
	}
	return string(str)
}

func (s *Scanner) scanInt() (int, error) {
	var number []rune

	for {
		r := s.read()
		if r == '.' && len(number) > 0 {
			return 0, fmt.Errorf("cannot use float as array index")
		}

		if isWhitespace(r) || r == '.' || r == ']' {
			s.unread()
			break
		}

		if !isDigit(r) {
			return 0, fmt.Errorf("non-integer value: %c", r)
		}

		number = append(number, r)
	}

	return strconv.Atoi(string(number))
}

func (s *Scanner) read() rune {
	ch, _, _ := s.buf.ReadRune()
	return ch
}

func (s *Scanner) unread() { _ = s.buf.UnreadRune() }

func isWhitespace(ch rune) bool { return ch == ' ' || ch == '\t' || ch == '\n' }

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}
