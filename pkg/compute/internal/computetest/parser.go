package computetest

import (
	"fmt"
	"strconv"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

const nullLit = "null"

type parser struct {
	alloc   *memory.Allocator
	scanner *scanner

	// Lookahead
	pos position
	tok token
	lit string
}

// Parse parses all test cases.
func (p *parser) Parse() ([]Case, error) { return p.parseCases() }

// next advances to the next token.
func (p *parser) next() {
	p.pos, p.tok, p.lit = p.scanner.Scan()
}

// expect consumes the next token and returns an error if it doesn't match expected.
func (p *parser) expect(expected token) error {
	if p.tok != expected {
		return fmt.Errorf("line %d:%d: expected %s, got %s", p.pos.Line, p.pos.Col, expected, p.tok)
	}
	p.next()
	return nil
}

// parseCases := Case*
func (p *parser) parseCases() ([]Case, error) {
	p.next() // Initialize lookahead

	var cases []Case
	for p.tok != tokenEOF {
		c, err := p.parseCase()
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}

	return cases, nil
}

// parseCase := Function Datum* Selection? "->" Datum TERMINATOR
func (p *parser) parseCase() (Case, error) {
	c := Case{Line: p.pos.Line}

	// Function
	if p.tok != tokenIdent {
		return Case{}, fmt.Errorf("line %d:%d: expected function name, got %s", p.pos.Line, p.pos.Col, p.tok)
	}
	c.Function = p.lit
	p.next()

	// Parse arguments until we hit "->" or "select"
	for p.tok != tokenArrow && p.tok != tokenSelect && p.tok != tokenEOF {
		datum, err := p.parseDatum()
		if err != nil {
			return Case{}, err
		}
		c.Arguments = append(c.Arguments, datum)
	}

	// Parse optional selection vector
	if p.tok == tokenSelect {
		selection, err := p.parseSelection()
		if err != nil {
			return Case{}, err
		}
		c.Selection = selection
	}

	if err := p.expect(tokenArrow); err != nil {
		return Case{}, err
	}

	expect, err := p.parseDatum()
	if err != nil {
		return Case{}, fmt.Errorf("line %d:%d: failed to parse expected result: %w", p.pos.Line, p.pos.Col, err)
	}
	c.Expect = expect

	// Expect terminator
	if err := p.expect(tokenTerm); err != nil {
		return Case{}, err
	}

	return c, nil
}

// parseDatum := BoolDatum | Int64Datum | Uint64Datum
func (p *parser) parseDatum() (columnar.Datum, error) {
	if p.tok != tokenIdent {
		return nil, fmt.Errorf("line %d:%d: expected kind (bool, int64, etc), got %s", p.pos.Line, p.pos.Col, p.tok)
	}

	kind := p.lit
	p.next()

	if err := p.expect(tokenColon); err != nil {
		return nil, err
	}

	switch kind {
	case "bool":
		return p.parseBoolDatum()
	case "int64":
		return p.parseInt64Datum()
	case "uint64":
		return p.parseUint64Datum()
	case "utf8":
		return p.parseUTF8Datum()
	case nullLit:
		return p.parseNullDatum()
	default:
		return nil, fmt.Errorf("line %d:%d: unknown kind %q", p.pos.Line, p.pos.Col, kind)
	}
}

// parseNullDatum := "null"
func (p *parser) parseNullDatum() (columnar.Datum, error) {
	if p.tok == tokenLBrack {
		return p.parseNullArray()
	}
	return p.parseNullScalar()
}

func (p *parser) parseNullArray() (columnar.Datum, error) {
	if err := p.expect(tokenLBrack); err != nil {
		return nil, err
	}

	builder := columnar.NewNullBuilder(p.alloc)

	for p.tok != tokenRBrack && p.tok != tokenEOF {
		_, err := p.parseNullScalar()
		if err != nil {
			return nil, err
		}
		builder.AppendNull()
	}

	if err := p.expect(tokenRBrack); err != nil {
		return nil, err
	}
	return builder.Build(), nil
}

func (p *parser) parseNullScalar() (columnar.Datum, error) {
	if p.tok != tokenIdent || p.lit != nullLit {
		return nil, fmt.Errorf("line %d:%d: expected 'null', got %s", p.pos.Line, p.pos.Col, p.lit)
	}
	p.next()
	return &columnar.NullScalar{}, nil
}

// parseBoolDatum := BoolValue
func (p *parser) parseBoolDatum() (columnar.Datum, error) {
	if p.tok == tokenLBrack {
		return p.parseBoolArray()
	}
	return p.parseBoolScalar()
}

// parseBoolScalar := "true" | "false" | "null"
func (p *parser) parseBoolScalar() (columnar.Datum, error) {
	if p.tok != tokenIdent {
		return nil, fmt.Errorf("line %d:%d: expected bool value, got %s", p.pos.Line, p.pos.Col, p.tok)
	}

	var scalar *columnar.BoolScalar
	switch p.lit {
	case "true":
		scalar = &columnar.BoolScalar{Value: true}
	case "false":
		scalar = &columnar.BoolScalar{Value: false}
	case nullLit:
		scalar = &columnar.BoolScalar{Null: true}
	default:
		return nil, fmt.Errorf("line %d:%d: expected true, false, or null, got %q", p.pos.Line, p.pos.Col, p.lit)
	}

	p.next()
	return scalar, nil
}

// parseBoolArray := "[" BoolScalar* "]"
func (p *parser) parseBoolArray() (*columnar.Bool, error) {
	if err := p.expect(tokenLBrack); err != nil {
		return nil, err
	}

	builder := columnar.NewBoolBuilder(p.alloc)

	for p.tok != tokenRBrack && p.tok != tokenEOF {
		scalar, err := p.parseBoolScalar()
		if err != nil {
			return nil, err
		}

		boolScalar := scalar.(*columnar.BoolScalar)
		if boolScalar.Null {
			builder.AppendNull()
		} else {
			builder.AppendValue(boolScalar.Value)
		}
	}

	if err := p.expect(tokenRBrack); err != nil {
		return nil, err
	}
	return builder.Build(), nil
}

// parseInt64Datum := NumberValue
func (p *parser) parseInt64Datum() (columnar.Datum, error) {
	if p.tok == tokenLBrack {
		return p.parseInt64Array()
	}
	return p.parseInt64Scalar()
}

// parseInt64Scalar := <number> | "null"
func (p *parser) parseInt64Scalar() (columnar.Datum, error) {
	if p.tok == tokenIdent && p.lit == nullLit {
		p.next()
		return &columnar.NumberScalar[int64]{Null: true}, nil
	}

	var negative bool
	if p.tok == tokenSub {
		negative = true
		p.next()
	}

	if p.tok != tokenInteger {
		return nil, fmt.Errorf("line %d:%d: expected integer, got %s", p.pos.Line, p.pos.Col, p.tok)
	}

	value, err := strconv.ParseInt(p.lit, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("line %d:%d: %w", p.pos.Line, p.pos.Col, err)
	}
	if negative {
		value = -value
	}

	p.next()
	return &columnar.NumberScalar[int64]{Value: value}, nil
}

// parseInt64Array := "[" Int64Scalar* "]"
func (p *parser) parseInt64Array() (columnar.Datum, error) {
	if err := p.expect(tokenLBrack); err != nil {
		return nil, err
	}

	builder := columnar.NewNumberBuilder[int64](p.alloc)

	for p.tok != tokenRBrack && p.tok != tokenEOF {
		scalar, err := p.parseInt64Scalar()
		if err != nil {
			return nil, err
		}

		int64Scalar := scalar.(*columnar.NumberScalar[int64])
		if int64Scalar.Null {
			builder.AppendNull()
		} else {
			builder.AppendValue(int64Scalar.Value)
		}
	}

	if err := p.expect(tokenRBrack); err != nil {
		return nil, err
	}

	return builder.Build(), nil
}

// parseUint64Datum := NumberValue
func (p *parser) parseUint64Datum() (columnar.Datum, error) {
	if p.tok == tokenLBrack {
		return p.parseUint64Array()
	}
	return p.parseUint64Scalar()
}

// parseUint64Scalar := <number> | "null"
func (p *parser) parseUint64Scalar() (columnar.Datum, error) {
	if p.tok == tokenIdent && p.lit == nullLit {
		p.next()
		return &columnar.NumberScalar[uint64]{Null: true}, nil
	}

	if p.tok != tokenInteger {
		return nil, fmt.Errorf("line %d:%d: expected integer, got %s", p.pos.Line, p.pos.Col, p.tok)
	}

	value, err := strconv.ParseUint(p.lit, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("line %d:%d: %w", p.pos.Line, p.pos.Col, err)
	}

	p.next()
	return &columnar.NumberScalar[uint64]{Value: value}, nil
}

// parseUint64Array := "[" Uint64Scalar* "]"
func (p *parser) parseUint64Array() (columnar.Datum, error) {
	if err := p.expect(tokenLBrack); err != nil {
		return nil, err
	}

	builder := columnar.NewNumberBuilder[uint64](p.alloc)

	for p.tok != tokenRBrack && p.tok != tokenEOF {
		scalar, err := p.parseUint64Scalar()
		if err != nil {
			return nil, err
		}

		uint64Scalar := scalar.(*columnar.NumberScalar[uint64])
		if uint64Scalar.Null {
			builder.AppendNull()
		} else {
			builder.AppendValue(uint64Scalar.Value)
		}
	}

	if err := p.expect(tokenRBrack); err != nil {
		return nil, err
	}
	return builder.Build(), nil
}

// parseUTF8Datum := UTF8Value
func (p *parser) parseUTF8Datum() (columnar.Datum, error) {
	if p.tok == tokenLBrack {
		return p.parseUTF8Array()
	}
	return p.parseUTF8Scalar()
}

// parseUTF8Scalar := <string> | "null"
func (p *parser) parseUTF8Scalar() (columnar.Datum, error) {
	if p.tok == tokenIdent && p.lit == nullLit {
		p.next()
		return &columnar.UTF8Scalar{Null: true}, nil
	}

	if p.tok != tokenString {
		return nil, fmt.Errorf("line %d:%d: expected quoted string value, got %s", p.pos.Line, p.pos.Col, p.tok)
	}

	value := []byte(p.lit)
	p.next()
	return &columnar.UTF8Scalar{Value: value}, nil
}

// parseUTF8Array := "[" UTF8Scalar* "]"
func (p *parser) parseUTF8Array() (columnar.Datum, error) {
	if err := p.expect(tokenLBrack); err != nil {
		return nil, err
	}

	builder := columnar.NewUTF8Builder(p.alloc)

	for p.tok != tokenRBrack && p.tok != tokenEOF {
		scalar, err := p.parseUTF8Scalar()
		if err != nil {
			return nil, err
		}

		utf8Scalar := scalar.(*columnar.UTF8Scalar)
		if utf8Scalar.Null {
			builder.AppendNull()
		} else {
			builder.AppendValue(utf8Scalar.Value)
		}
	}

	if err := p.expect(tokenRBrack); err != nil {
		return nil, err
	}
	return builder.Build(), nil
}

// parseSelection := "select" ":" parseBoolArray
func (p *parser) parseSelection() (memory.Bitmap, error) {
	if err := p.expect(tokenSelect); err != nil {
		return memory.Bitmap{}, err
	} else if err := p.expect(tokenColon); err != nil {
		return memory.Bitmap{}, err
	}

	datum, err := p.parseBoolArray()
	if err != nil {
		return memory.Bitmap{}, err
	} else if datum.Nulls() > 0 {
		return memory.Bitmap{}, fmt.Errorf("line %d:%d: selection cannot contain null values", p.pos.Line, p.pos.Col)
	}

	return datum.Values(), nil
}
