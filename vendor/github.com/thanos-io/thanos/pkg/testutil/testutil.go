// Copyright (c) The Thanos Authors.
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
	"github.com/pkg/errors"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"go.uber.org/goleak"
)

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
	tb.Fatalf("\033[31m%s:%d: "+msg+"\033[39m\n\n", filepath.Base(file), line)
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
	tb.Fatalf("\033[31m%s:%d:"+msg+"\n\n unexpected error: %s\033[39m\n\n", filepath.Base(file), line, err.Error())
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
	tb.Fatalf("\033[31m%s:%d:"+msg+"\n\n expected error, got nothing \033[39m\n\n", filepath.Base(file), line)
}

// Equals fails the test if exp is not equal to act.
func Equals(tb testing.TB, exp, act interface{}, v ...interface{}) {
	tb.Helper()
	if reflect.DeepEqual(exp, act) {
		return
	}
	_, file, line, _ := runtime.Caller(1)

	var msg string
	if len(v) > 0 {
		msg = fmt.Sprintf(v[0].(string), v[1:]...)
	}
	tb.Fatal(sprintfWithLimit("\033[31m%s:%d:"+msg+"\n\n\texp: %#v\n\n\tgot: %#v%s\033[39m\n\n", filepath.Base(file), line, exp, act, diff(exp, act)))
}

func sprintfWithLimit(act string, v ...interface{}) string {
	s := fmt.Sprintf(act, v...)
	if len(s) > 10000 {
		return s[:10000] + "...(output trimmed)"
	}
	return s
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

	diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(e),
		B:        difflib.SplitLines(a),
		FromFile: "Expected",
		FromDate: "",
		ToFile:   "Actual",
		ToDate:   "",
		Context:  1,
	})
	return "\n\nDiff:\n" + diff
}

// GatherAndCompare compares the metrics of a Gatherers pair.
func GatherAndCompare(t *testing.T, g1 prometheus.Gatherer, g2 prometheus.Gatherer, filter string) {
	g1m, err := g1.Gather()
	Ok(t, err)
	g2m, err := g2.Gather()
	Ok(t, err)

	var m1 *dto.MetricFamily
	for _, m := range g1m {
		if *m.Name == filter {
			m1 = m
		}
	}
	var m2 *dto.MetricFamily
	for _, m := range g2m {
		if *m.Name == filter {
			m2 = m
		}
	}
	Equals(t, m1.String(), m2.String())
}

// TolerantVerifyLeakMain verifies go leaks but excludes the go routines that are
// launched as side effects of some of our dependencies.
func TolerantVerifyLeakMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// https://github.com/census-instrumentation/opencensus-go/blob/d7677d6af5953e0506ac4c08f349c62b917a443a/stats/view/worker.go#L34
		goleak.IgnoreTopFunction("go.opencensus.io/stats/view.(*worker).start"),
		// https://github.com/kubernetes/klog/blob/c85d02d1c76a9ebafa81eb6d35c980734f2c4727/klog.go#L417
		goleak.IgnoreTopFunction("k8s.io/klog/v2.(*loggingT).flushDaemon"),
		goleak.IgnoreTopFunction("k8s.io/klog.(*loggingT).flushDaemon"),
	)
}

// TolerantVerifyLeak verifies go leaks but excludes the go routines that are
// launched as side effects of some of our dependencies.
func TolerantVerifyLeak(t *testing.T) {
	goleak.VerifyNone(t,
		// https://github.com/census-instrumentation/opencensus-go/blob/d7677d6af5953e0506ac4c08f349c62b917a443a/stats/view/worker.go#L34
		goleak.IgnoreTopFunction("go.opencensus.io/stats/view.(*worker).start"),
		// https://github.com/kubernetes/klog/blob/c85d02d1c76a9ebafa81eb6d35c980734f2c4727/klog.go#L417
		goleak.IgnoreTopFunction("k8s.io/klog/v2.(*loggingT).flushDaemon"),
		goleak.IgnoreTopFunction("k8s.io/klog.(*loggingT).flushDaemon"),
	)
}

// FaultOrPanicToErr returns error if panic of fault was triggered during execution of function.
func FaultOrPanicToErr(f func()) (err error) {
	// Set this go routine to panic on segfault to allow asserting on those.
	debug.SetPanicOnFault(true)
	defer func() {
		if r := recover(); r != nil {
			err = errors.Errorf("invoked function panicked or caused segmentation fault: %v", r)
		}
		debug.SetPanicOnFault(false)
	}()

	f()

	return err
}
