// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package confmap // import "go.opentelemetry.io/collector/confmap"

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/collector/confmap/internal"
)

// schemePattern defines the regexp pattern for scheme names.
// Scheme name consist of a sequence of characters beginning with a letter and followed by any
// combination of letters, digits, plus ("+"), period ("."), or hyphen ("-").
const schemePattern = `[A-Za-z][A-Za-z0-9+.-]+`

var (
	// Need to match new line as well in the OpaqueValue, so setting the "s" flag. See https://pkg.go.dev/regexp/syntax.
	uriRegexp = regexp.MustCompile(`(?s:^(?P<Scheme>` + schemePattern + `):(?P<OpaqueValue>.*)$)`)

	errTooManyRecursiveExpansions = errors.New("too many recursive expansions")
)

func (mr *Resolver) expandValueRecursively(ctx context.Context, value any) (any, error) {
	for range 1000 {
		val, changed, err := mr.expandValue(ctx, value)
		if err != nil {
			return nil, err
		}
		if !changed {
			return val, nil
		}
		value = val
	}
	return nil, errTooManyRecursiveExpansions
}

func (mr *Resolver) expandValue(ctx context.Context, value any) (any, bool, error) {
	switch v := value.(type) {
	case internal.ExpandedValue:
		expanded, changed, err := mr.expandValue(ctx, v.Value)
		if err != nil {
			return nil, false, err
		}

		switch exp := expanded.(type) {
		case internal.ExpandedValue, string:
			// Return expanded values or strings verbatim.
			return exp, changed, nil
		}

		// At this point we don't know the target field type, so we need to expand the original representation as well.
		originalExpanded, originalChanged, err := mr.expandValue(ctx, v.Original)
		if err != nil {
			// The original representation is not valid, return the expanded value.
			return expanded, changed, nil
		}

		if originalExpanded, ok := originalExpanded.(string); ok {
			// If the original representation is a string, return the expanded value with the original representation.
			return internal.ExpandedValue{
				Value:    expanded,
				Original: originalExpanded,
			}, changed || originalChanged, nil
		}

		return expanded, changed, nil
	case string:
		if !strings.Contains(v, "${") || !strings.Contains(v, "}") {
			// No URIs to expand.
			return value, false, nil
		}
		// Embedded or nested URIs.
		return mr.findAndExpandURI(ctx, v)
	case []any:
		nslice := make([]any, 0, len(v))
		nchanged := false
		for _, vint := range v {
			val, changed, err := mr.expandValue(ctx, vint)
			if err != nil {
				return nil, false, err
			}
			nslice = append(nslice, val)
			nchanged = nchanged || changed
		}
		return nslice, nchanged, nil
	case map[string]any:
		nmap := map[string]any{}
		nchanged := false
		for mk, mv := range v {
			val, changed, err := mr.expandValue(ctx, mv)
			if err != nil {
				return nil, false, err
			}
			nmap[mk] = val
			nchanged = nchanged || changed
		}
		return nmap, nchanged, nil
	}
	return value, false, nil
}

// findURI attempts to find the first potentially expandable URI in input. It returns a potentially expandable
// URI, or an empty string if none are found.
// Note: findURI is only called when input contains a closing bracket.
// We do not support escaping nested URIs (such as ${env:$${FOO}}, since that would result in an invalid outer URI (${env:${FOO}}).
func (mr *Resolver) findURI(input string) string {
	closeIndex := strings.Index(input, "}")
	remaining := input[closeIndex+1:]
	openIndex := strings.LastIndex(input[:closeIndex+1], "${")

	// if there is any of:
	//  - a missing "${"
	//  - there is no default scheme AND no scheme is detected because no `:` is found.
	// then check the next URI.
	if openIndex < 0 || (mr.defaultScheme == "" && !strings.Contains(input[openIndex:closeIndex+1], ":")) {
		// if remaining does not contain "}", there are no URIs left: stop recursion.
		if !strings.Contains(remaining, "}") {
			return ""
		}
		return mr.findURI(remaining)
	}

	index := openIndex - 1
	currentRune := '$'
	count := 0
	for index >= 0 && currentRune == '$' {
		currentRune = rune(input[index])
		if currentRune == '$' {
			count++
		}
		index--
	}
	// if we found an odd number of immediately $ preceding ${, then the expansion is escaped
	if count%2 == 1 {
		return ""
	}

	return input[openIndex : closeIndex+1]
}

// findAndExpandURI attempts to find and expand the first occurrence of an expandable URI in input. If an expandable URI is found it
// returns the input with the URI expanded, true and nil. Otherwise, it returns the unchanged input, false and the expanding error.
// This method expects input to start with ${ and end with }
func (mr *Resolver) findAndExpandURI(ctx context.Context, input string) (any, bool, error) {
	uri := mr.findURI(input)
	if uri == "" {
		// No URI found, return.
		return input, false, nil
	}
	if uri == input {
		// If the value is a single URI, then the return value can be anything.
		// This is the case `foo: ${file:some_extra_config.yml}`.
		ret, err := mr.expandURI(ctx, input)
		if err != nil {
			return input, false, err
		}

		val, err := ret.AsRaw()
		if err != nil {
			return input, false, err
		}

		if asStr, err2 := ret.AsString(); err2 == nil {
			return internal.ExpandedValue{
				Value:    val,
				Original: asStr,
			}, true, nil
		}

		return val, true, nil
	}
	expanded, err := mr.expandURI(ctx, uri)
	if err != nil {
		return input, false, err
	}

	repl, err := expanded.AsString()
	if err != nil {
		return input, false, fmt.Errorf("expanding %v: %w", uri, err)
	}
	return strings.ReplaceAll(input, uri, repl), true, err
}

func (mr *Resolver) expandURI(ctx context.Context, input string) (*Retrieved, error) {
	// strip ${ and }
	uri := input[2 : len(input)-1]

	if !strings.Contains(uri, ":") {
		uri = fmt.Sprintf("%s:%s", mr.defaultScheme, uri)
	}

	lURI, err := newLocation(uri)
	if err != nil {
		return nil, err
	}

	if strings.Contains(lURI.opaqueValue, "$") {
		return nil, fmt.Errorf("the uri %q contains unsupported characters ('$')", lURI.asString())
	}
	ret, err := mr.retrieveValue(ctx, lURI)
	if err != nil {
		return nil, err
	}
	mr.closers = append(mr.closers, ret.Close)
	return ret, nil
}

type location struct {
	scheme      string
	opaqueValue string
}

func (c location) asString() string {
	return c.scheme + ":" + c.opaqueValue
}

func newLocation(uri string) (location, error) {
	submatches := uriRegexp.FindStringSubmatch(uri)
	if len(submatches) != 3 {
		return location{}, fmt.Errorf("invalid uri: %q", uri)
	}
	return location{scheme: submatches[1], opaqueValue: submatches[2]}, nil
}
