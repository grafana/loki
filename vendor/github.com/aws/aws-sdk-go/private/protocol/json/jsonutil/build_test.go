package jsonutil_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/private/protocol/json/jsonutil"
)

func S(s string) *string {
	return &s
}

func D(s int64) *int64 {
	return &s
}

func F(s float64) *float64 {
	return &s
}

func T(s time.Time) *time.Time {
	return &s
}

type J struct {
	S  *string
	SS []string
	D  *int64
	F  *float64
	T  *time.Time
}

var zero = 0.0

var jsonTests = []struct {
	in  interface{}
	out string
	err string
}{
	{
		J{},
		`{}`,
		``,
	},
	{
		J{
			S:  S("str"),
			SS: []string{"A", "B", "C"},
			D:  D(123),
			F:  F(4.56),
			T:  T(time.Unix(987, 0)),
		},
		`{"S":"str","SS":["A","B","C"],"D":123,"F":4.56,"T":987}`,
		``,
	},
	{
		J{
			S: S(`"''"`),
		},
		`{"S":"\"''\""}`,
		``,
	},
	{
		J{
			S: S("\x00føø\u00FF\n\\\"\r\t\b\f"),
		},
		`{"S":"\u0000føøÿ\n\\\"\r\t\b\f"}`,
		``,
	},
	{
		J{
			F: F(4.56 / zero),
		},
		"",
		`json: unsupported value: +Inf`,
	},
}

func TestBuildJSON(t *testing.T) {
	for _, test := range jsonTests {
		out, err := jsonutil.BuildJSON(test.in)
		if test.err != "" {
			if err == nil {
				t.Errorf("expect error")
			}
			if e, a := test.err, err.Error(); !strings.Contains(a, e) {
				t.Errorf("expect %v, to contain %v", e, a)
			}
		} else {
			if err != nil {
				t.Errorf("expect nil, %v", err)
			}
			if e, a := string(out), test.out; e != a {
				t.Errorf("expect %v, got %v", e, a)
			}
		}
	}
}

func BenchmarkBuildJSON(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for _, test := range jsonTests {
			jsonutil.BuildJSON(test.in)
		}
	}
}

func BenchmarkStdlibJSON(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for _, test := range jsonTests {
			json.Marshal(test.in)
		}
	}
}
