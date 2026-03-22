// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package testutil

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/efficientgo/core/errors"
	"github.com/efficientgo/core/testutil/internal"
	"github.com/google/go-cmp/cmp"
)

const limitOfElemChars = 1e3

func withLimitf(f string, v ...interface{}) string {
	s := fmt.Sprintf(f, v...)
	if len(s) > limitOfElemChars {
		return s[:limitOfElemChars] + "...(output trimmed)"
	}
	return s
}

// Assert fails the test if the condition is false.
func Assert(tb testing.TB, condition bool, v ...interface{}) {
	tb.Helper()
	if condition {
		return
	}
	_, file, line, _ := runtime.Caller(1)

	var msg string
	if len(v) > 0 {
		msg = fmt.Sprintf(v[0].(string), v[1:]...)
	}
	tb.Fatalf("\033[31m%s:%d: \"%s\"\033[39m\n\n", filepath.Base(file), line, withLimitf(msg))
}

// Ok fails the test if an err is not nil.
func Ok(tb testing.TB, err error, v ...interface{}) {
	tb.Helper()
	if err == nil {
		return
	}
	_, file, line, _ := runtime.Caller(1)

	var msg string
	if len(v) > 0 {
		msg = fmt.Sprintf(v[0].(string), v[1:]...)
	}
	tb.Fatalf("\033[31m%s:%d: \"%s\"\n\n unexpected error: %s\033[39m\n\n", filepath.Base(file), line, withLimitf(msg), withLimitf(err.Error()))
}

// NotOk fails the test if an err is nil.
func NotOk(tb testing.TB, err error, v ...interface{}) {
	tb.Helper()
	if err != nil {
		return
	}
	_, file, line, _ := runtime.Caller(1)

	var msg string
	if len(v) > 0 {
		msg = fmt.Sprintf(v[0].(string), v[1:]...)
	}
	tb.Fatalf("\033[31m%s:%d: \"%s\"\n\n expected error, got nothing \033[39m\n\n", filepath.Base(file), line, withLimitf(msg))
}

// Equals fails the test if exp is not equal to act.
func Equals(tb testing.TB, exp, act interface{}, v ...interface{}) {
	tb.Helper()
	if reflect.DeepEqual(exp, act) {
		return
	}
	fatalNotEqual(tb, exp, act, v...)
}

func fatalNotEqual(tb testing.TB, exp, act interface{}, v ...interface{}) {
	_, file, line, _ := runtime.Caller(2)

	var msg string
	if len(v) > 0 {
		msg = fmt.Sprintf(v[0].(string), v[1:]...)
	}
	tb.Fatalf(
		"\033[31m%s:%d: \"%s\"\n\n\texp: %s\n\n\tgot: %s%s\033[39m\n\n",
		filepath.Base(file), line, withLimitf(msg), withLimitf("%#v", exp), withLimitf("%#v", act), withLimitf(diff(exp, act)),
	)
}

type goCmp struct {
	opts cmp.Options
}

// WithGoCmp allows specifying options and using https://github.com/google/go-cmp
// for equality comparisons. The compatibility guarantee of this function's arguments
// are the same as go-cmp (no guarantee due to v0.x).
func WithGoCmp(opts ...cmp.Option) goCmp {
	return goCmp{opts: opts}
}

// Equals uses go-cmp for comparing equality between two structs, and can be used with
// various options defined in go-cmp/cmp and go-cmp/cmp/cmpopts.
func (o goCmp) Equals(tb testing.TB, exp, act interface{}, v ...interface{}) {
	tb.Helper()
	if cmp.Equal(exp, act, o.opts) {
		return
	}
	fatalNotEqual(tb, exp, act, v...)
}

// FaultOrPanicToErr returns error if panic of fault was triggered during execution of function.
func FaultOrPanicToErr(f func()) (err error) {
	// Set this go routine to panic on segfault to allow asserting on those.
	debug.SetPanicOnFault(true)
	defer func() {
		if r := recover(); r != nil {
			err = errors.Newf("invoked function panicked or caused segmentation fault: %v", r)
		}
		debug.SetPanicOnFault(false)
	}()

	f()

	return err
}

// ContainsStringSlice fails the test if needle is not contained within haystack, if haystack or needle is
// an empty slice, or if needle is longer than haystack.
func ContainsStringSlice(tb testing.TB, haystack, needle []string) {
	_, file, line, _ := runtime.Caller(1)

	if !contains(haystack, needle) {
		tb.Fatalf("\033[31m%s:%d: %s does not contain %s\033[39m\n\n", filepath.Base(file), line, withLimitf("%#v", haystack), withLimitf("%#v", needle))
	}
}

func contains(haystack, needle []string) bool {
	if len(haystack) == 0 || len(needle) == 0 {
		return false
	}

	if len(haystack) < len(needle) {
		return false
	}

	for i := 0; i < len(haystack); i++ {
		outer := i

		for j := 0; j < len(needle); j++ {
			// End of the haystack but not the end of the needle, end
			if outer == len(haystack) {
				return false
			}

			// No match, try the next index of the haystack
			if haystack[outer] != needle[j] {
				break
			}

			// End of the needle and it still matches, end
			if j == len(needle)-1 {
				return true
			}

			// This element matches between the two slices, try the next one
			outer++
		}
	}

	return false
}

func typeAndKind(v interface{}) (reflect.Type, reflect.Kind) {
	t := reflect.TypeOf(v)
	k := t.Kind()

	if k == reflect.Ptr {
		t = t.Elem()
		k = t.Kind()
	}
	return t, k
}

// diff returns a diff of both values as long as both are of the same type and
// are a struct, map, slice, array or string. Otherwise it returns an empty string.
func diff(expected interface{}, actual interface{}) string {
	if expected == nil || actual == nil {
		return ""
	}

	et, ek := typeAndKind(expected)
	at, _ := typeAndKind(actual)
	if et != at {
		return ""
	}

	if ek != reflect.Struct && ek != reflect.Map && ek != reflect.Slice && ek != reflect.Array && ek != reflect.String {
		return ""
	}

	var e, a string
	c := spew.ConfigState{
		Indent:                  " ",
		DisablePointerAddresses: true,
		DisableCapacities:       true,
		SortKeys:                true,
	}
	if et != reflect.TypeOf("") {
		e = c.Sdump(expected)
		a = c.Sdump(actual)
	} else {
		e = reflect.ValueOf(expected).String()
		a = reflect.ValueOf(actual).String()
	}

	diff, _ := internal.GetUnifiedDiffString(internal.UnifiedDiff{
		A:        internal.SplitLines(e),
		B:        internal.SplitLines(a),
		FromFile: "Expected",
		FromDate: "",
		ToFile:   "Actual",
		ToDate:   "",
		Context:  1,
	})
	return "\n\nDiff:\n" + diff
}
