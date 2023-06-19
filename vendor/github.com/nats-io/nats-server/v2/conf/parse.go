// Copyright 2013-2018 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package conf supports a configuration file format used by gnatsd. It is
// a flexible format that combines the best of traditional
// configuration formats and newer styles such as JSON and YAML.
package conf

// The format supported is less restrictive than today's formats.
// Supports mixed Arrays [], nested Maps {}, multiple comment types (# and //)
// Also supports key value assignments using '=' or ':' or whiteSpace()
//   e.g. foo = 2, foo : 2, foo 2
// maps can be assigned with no key separator as well
// semicolons as value terminators in key/value assignments are optional
//
// see parse_test.go for more examples.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type parser struct {
	mapping map[string]interface{}
	lx      *lexer

	// The current scoped context, can be array or map
	ctx interface{}

	// stack of contexts, either map or array/slice stack
	ctxs []interface{}

	// Keys stack
	keys []string

	// Keys stack as items
	ikeys []item

	// The config file path, empty by default.
	fp string

	// pedantic reports error when configuration is not correct.
	pedantic bool
}

// Parse will return a map of keys to interface{}, although concrete types
// underly them. The values supported are string, bool, int64, float64, DateTime.
// Arrays and nested Maps are also supported.
func Parse(data string) (map[string]interface{}, error) {
	p, err := parse(data, "", false)
	if err != nil {
		return nil, err
	}
	return p.mapping, nil
}

// ParseFile is a helper to open file, etc. and parse the contents.
func ParseFile(fp string) (map[string]interface{}, error) {
	data, err := os.ReadFile(fp)
	if err != nil {
		return nil, fmt.Errorf("error opening config file: %v", err)
	}

	p, err := parse(string(data), fp, false)
	if err != nil {
		return nil, err
	}
	return p.mapping, nil
}

// ParseFileWithChecks is equivalent to ParseFile but runs in pedantic mode.
func ParseFileWithChecks(fp string) (map[string]interface{}, error) {
	data, err := os.ReadFile(fp)
	if err != nil {
		return nil, err
	}

	p, err := parse(string(data), fp, true)
	if err != nil {
		return nil, err
	}

	return p.mapping, nil
}

type token struct {
	item         item
	value        interface{}
	usedVariable bool
	sourceFile   string
}

func (t *token) Value() interface{} {
	return t.value
}

func (t *token) Line() int {
	return t.item.line
}

func (t *token) IsUsedVariable() bool {
	return t.usedVariable
}

func (t *token) SourceFile() string {
	return t.sourceFile
}

func (t *token) Position() int {
	return t.item.pos
}

func parse(data, fp string, pedantic bool) (p *parser, err error) {
	p = &parser{
		mapping:  make(map[string]interface{}),
		lx:       lex(data),
		ctxs:     make([]interface{}, 0, 4),
		keys:     make([]string, 0, 4),
		ikeys:    make([]item, 0, 4),
		fp:       filepath.Dir(fp),
		pedantic: pedantic,
	}
	p.pushContext(p.mapping)

	for {
		it := p.next()
		if it.typ == itemEOF {
			break
		}
		if err := p.processItem(it, fp); err != nil {
			return nil, err
		}
	}

	return p, nil
}

func (p *parser) next() item {
	return p.lx.nextItem()
}

func (p *parser) pushContext(ctx interface{}) {
	p.ctxs = append(p.ctxs, ctx)
	p.ctx = ctx
}

func (p *parser) popContext() interface{} {
	if len(p.ctxs) == 0 {
		panic("BUG in parser, context stack empty")
	}
	li := len(p.ctxs) - 1
	last := p.ctxs[li]
	p.ctxs = p.ctxs[0:li]
	p.ctx = p.ctxs[len(p.ctxs)-1]
	return last
}

func (p *parser) pushKey(key string) {
	p.keys = append(p.keys, key)
}

func (p *parser) popKey() string {
	if len(p.keys) == 0 {
		panic("BUG in parser, keys stack empty")
	}
	li := len(p.keys) - 1
	last := p.keys[li]
	p.keys = p.keys[0:li]
	return last
}

func (p *parser) pushItemKey(key item) {
	p.ikeys = append(p.ikeys, key)
}

func (p *parser) popItemKey() item {
	if len(p.ikeys) == 0 {
		panic("BUG in parser, item keys stack empty")
	}
	li := len(p.ikeys) - 1
	last := p.ikeys[li]
	p.ikeys = p.ikeys[0:li]
	return last
}

func (p *parser) processItem(it item, fp string) error {
	setValue := func(it item, v interface{}) {
		if p.pedantic {
			p.setValue(&token{it, v, false, fp})
		} else {
			p.setValue(v)
		}
	}

	switch it.typ {
	case itemError:
		return fmt.Errorf("Parse error on line %d: '%s'", it.line, it.val)
	case itemKey:
		// Keep track of the keys as items and strings,
		// we do this in order to be able to still support
		// includes without many breaking changes.
		p.pushKey(it.val)

		if p.pedantic {
			p.pushItemKey(it)
		}
	case itemMapStart:
		newCtx := make(map[string]interface{})
		p.pushContext(newCtx)
	case itemMapEnd:
		setValue(it, p.popContext())
	case itemString:
		// FIXME(dlc) sanitize string?
		setValue(it, it.val)
	case itemInteger:
		lastDigit := 0
		for _, r := range it.val {
			if !unicode.IsDigit(r) && r != '-' {
				break
			}
			lastDigit++
		}
		numStr := it.val[:lastDigit]
		num, err := strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			if e, ok := err.(*strconv.NumError); ok &&
				e.Err == strconv.ErrRange {
				return fmt.Errorf("integer '%s' is out of the range", it.val)
			}
			return fmt.Errorf("expected integer, but got '%s'", it.val)
		}
		// Process a suffix
		suffix := strings.ToLower(strings.TrimSpace(it.val[lastDigit:]))

		switch suffix {
		case "":
			setValue(it, num)
		case "k":
			setValue(it, num*1000)
		case "kb", "ki", "kib":
			setValue(it, num*1024)
		case "m":
			setValue(it, num*1000*1000)
		case "mb", "mi", "mib":
			setValue(it, num*1024*1024)
		case "g":
			setValue(it, num*1000*1000*1000)
		case "gb", "gi", "gib":
			setValue(it, num*1024*1024*1024)
		case "t":
			setValue(it, num*1000*1000*1000*1000)
		case "tb", "ti", "tib":
			setValue(it, num*1024*1024*1024*1024)
		case "p":
			setValue(it, num*1000*1000*1000*1000*1000)
		case "pb", "pi", "pib":
			setValue(it, num*1024*1024*1024*1024*1024)
		case "e":
			setValue(it, num*1000*1000*1000*1000*1000*1000)
		case "eb", "ei", "eib":
			setValue(it, num*1024*1024*1024*1024*1024*1024)
		}
	case itemFloat:
		num, err := strconv.ParseFloat(it.val, 64)
		if err != nil {
			if e, ok := err.(*strconv.NumError); ok &&
				e.Err == strconv.ErrRange {
				return fmt.Errorf("float '%s' is out of the range", it.val)
			}
			return fmt.Errorf("expected float, but got '%s'", it.val)
		}
		setValue(it, num)
	case itemBool:
		switch strings.ToLower(it.val) {
		case "true", "yes", "on":
			setValue(it, true)
		case "false", "no", "off":
			setValue(it, false)
		default:
			return fmt.Errorf("expected boolean value, but got '%s'", it.val)
		}

	case itemDatetime:
		dt, err := time.Parse("2006-01-02T15:04:05Z", it.val)
		if err != nil {
			return fmt.Errorf(
				"expected Zulu formatted DateTime, but got '%s'", it.val)
		}
		setValue(it, dt)
	case itemArrayStart:
		var array = make([]interface{}, 0)
		p.pushContext(array)
	case itemArrayEnd:
		array := p.ctx
		p.popContext()
		setValue(it, array)
	case itemVariable:
		value, found, err := p.lookupVariable(it.val)
		if err != nil {
			return fmt.Errorf("variable reference for '%s' on line %d could not be parsed: %s",
				it.val, it.line, err)
		}
		if !found {
			return fmt.Errorf("variable reference for '%s' on line %d can not be found",
				it.val, it.line)
		}

		if p.pedantic {
			switch tk := value.(type) {
			case *token:
				// Mark the looked up variable as used, and make
				// the variable reference become handled as a token.
				tk.usedVariable = true
				p.setValue(&token{it, tk.Value(), false, fp})
			default:
				// Special case to add position context to bcrypt references.
				p.setValue(&token{it, value, false, fp})
			}
		} else {
			p.setValue(value)
		}
	case itemInclude:
		var (
			m   map[string]interface{}
			err error
		)
		if p.pedantic {
			m, err = ParseFileWithChecks(filepath.Join(p.fp, it.val))
		} else {
			m, err = ParseFile(filepath.Join(p.fp, it.val))
		}
		if err != nil {
			return fmt.Errorf("error parsing include file '%s', %v", it.val, err)
		}
		for k, v := range m {
			p.pushKey(k)

			if p.pedantic {
				switch tk := v.(type) {
				case *token:
					p.pushItemKey(tk.item)
				}
			}
			p.setValue(v)
		}
	}

	return nil
}

// Used to map an environment value into a temporary map to pass to secondary Parse call.
const pkey = "pk"

// We special case raw strings here that are bcrypt'd. This allows us not to force quoting the strings
const bcryptPrefix = "2a$"

// lookupVariable will lookup a variable reference. It will use block scoping on keys
// it has seen before, with the top level scoping being the environment variables. We
// ignore array contexts and only process the map contexts..
//
// Returns true for ok if it finds something, similar to map.
func (p *parser) lookupVariable(varReference string) (interface{}, bool, error) {
	// Do special check to see if it is a raw bcrypt string.
	if strings.HasPrefix(varReference, bcryptPrefix) {
		return "$" + varReference, true, nil
	}

	// Loop through contexts currently on the stack.
	for i := len(p.ctxs) - 1; i >= 0; i-- {
		ctx := p.ctxs[i]
		// Process if it is a map context
		if m, ok := ctx.(map[string]interface{}); ok {
			if v, ok := m[varReference]; ok {
				return v, ok, nil
			}
		}
	}

	// If we are here, we have exhausted our context maps and still not found anything.
	// Parse from the environment.
	if vStr, ok := os.LookupEnv(varReference); ok {
		// Everything we get here will be a string value, so we need to process as a parser would.
		if vmap, err := Parse(fmt.Sprintf("%s=%s", pkey, vStr)); err == nil {
			v, ok := vmap[pkey]
			return v, ok, nil
		} else {
			return nil, false, err
		}
	}
	return nil, false, nil
}

func (p *parser) setValue(val interface{}) {
	// Test to see if we are on an array or a map

	// Array processing
	if ctx, ok := p.ctx.([]interface{}); ok {
		p.ctx = append(ctx, val)
		p.ctxs[len(p.ctxs)-1] = p.ctx
	}

	// Map processing
	if ctx, ok := p.ctx.(map[string]interface{}); ok {
		key := p.popKey()

		if p.pedantic {
			// Change the position to the beginning of the key
			// since more useful when reporting errors.
			switch v := val.(type) {
			case *token:
				it := p.popItemKey()
				v.item.pos = it.pos
				v.item.line = it.line
				ctx[key] = v
			}
		} else {
			// FIXME(dlc), make sure to error if redefining same key?
			ctx[key] = val
		}
	}
}
