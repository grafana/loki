package print

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logcli/output"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/util/marshal"
)

func Test_commonLabels(t *testing.T) {
	type args struct {
		lss []loghttp.LabelSet
	}
	tests := []struct {
		name string
		args args
		want loghttp.LabelSet
	}{
		{
			"Extract common labels source > target",
			args{
				[]loghttp.LabelSet{mustParseLabels(t, `{foo="bar", bar="foo"}`), mustParseLabels(t, `{bar="foo", foo="foo", baz="baz"}`)},
			},
			mustParseLabels(t, `{bar="foo"}`),
		},
		{
			"Extract common labels source > target",
			args{
				[]loghttp.LabelSet{mustParseLabels(t, `{foo="bar", bar="foo"}`), mustParseLabels(t, `{bar="foo", foo="bar", baz="baz"}`)},
			},
			mustParseLabels(t, `{foo="bar", bar="foo"}`),
		},
		{
			"Extract common labels source < target",
			args{
				[]loghttp.LabelSet{mustParseLabels(t, `{foo="bar", bar="foo"}`), mustParseLabels(t, `{bar="foo"}`)},
			},
			mustParseLabels(t, `{bar="foo"}`),
		},
		{
			"Extract common labels source < target no common",
			args{
				[]loghttp.LabelSet{mustParseLabels(t, `{foo="bar", bar="foo"}`), mustParseLabels(t, `{fo="bar"}`)},
			},
			loghttp.LabelSet{},
		},
		{
			"Extract common labels source = target no common",
			args{
				[]loghttp.LabelSet{mustParseLabels(t, `{foo="bar"}`), mustParseLabels(t, `{fooo="bar"}`)},
			},
			loghttp.LabelSet{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var streams []loghttp.Stream

			for _, lss := range tt.args.lss {
				streams = append(streams, loghttp.Stream{
					Entries: nil,
					Labels:  lss,
				})
			}

			if got := commonLabels(streams); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("commonLabels() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_subtract(t *testing.T) {
	type args struct {
		a loghttp.LabelSet
		b loghttp.LabelSet
	}
	tests := []struct {
		name string
		args args
		want loghttp.LabelSet
	}{
		{
			"Subtract labels source > target",
			args{
				mustParseLabels(t, `{foo="bar", bar="foo"}`),
				mustParseLabels(t, `{bar="foo", foo="foo", baz="baz"}`),
			},
			mustParseLabels(t, `{foo="bar"}`),
		},
		{
			"Subtract labels source < target",
			args{
				mustParseLabels(t, `{foo="bar", bar="foo"}`),
				mustParseLabels(t, `{bar="foo"}`),
			},
			mustParseLabels(t, `{foo="bar"}`),
		},
		{
			"Subtract labels source < target no sub",
			args{
				mustParseLabels(t, `{foo="bar", bar="foo"}`),
				mustParseLabels(t, `{fo="bar"}`),
			},
			mustParseLabels(t, `{bar="foo", foo="bar"}`),
		},
		{
			"Subtract labels source = target no sub",
			args{
				mustParseLabels(t, `{foo="bar"}`),
				mustParseLabels(t, `{fiz="buz"}`),
			},
			mustParseLabels(t, `{foo="bar"}`),
		},
		{
			"Subtract labels source > target no sub",
			args{
				mustParseLabels(t, `{foo="bar"}`),
				mustParseLabels(t, `{fiz="buz", foo="baz"}`),
			},
			mustParseLabels(t, `{foo="bar"}`),
		},
		{
			"Subtract labels source > target no sub",
			args{
				mustParseLabels(t, `{a="b", foo="bar", baz="baz", fizz="fizz"}`),
				mustParseLabels(t, `{foo="bar", baz="baz", buzz="buzz", fizz="fizz"}`),
			},
			mustParseLabels(t, `{a="b"}`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subtract(tt.args.a, tt.args.b); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subtract() = %v, want %v", got, tt.want)
			}
		})
	}
}

func mustParseLabels(t *testing.T, s string) loghttp.LabelSet {
	t.Helper()
	l, err := marshal.NewLabelSet(s)
	require.NoErrorf(t, err, "Failed to parse %q", s)

	return l
}

// Entries from different streams that share a timestamp and a line must not be
// treated as duplicates of each other when a batch boundary falls on that timestamp.
func TestPrintResult_OverlapKeepsSameLineFromOtherStream(t *testing.T) {
	ts := time.Unix(100, 0)
	streamA := loghttp.LabelSet{"app": "a"}
	streamB := loghttp.LabelSet{"app": "b"}

	buf := &bytes.Buffer{}
	out, err := output.NewLogOutput(buf, "raw", &output.LogOutputOptions{NoLabels: true})
	require.NoError(t, err)
	p := NewQueryResultPrinter(nil, nil, true, 0, true, false)

	// First batch ended after stream a's entry at ts, before stream b's.
	_, last, err := p.PrintResult(loghttp.Streams{
		{Labels: streamA, Entries: []loghttp.Entry{{Timestamp: ts, Line: "same"}}},
	}, out, nil)
	require.NoError(t, err)
	require.Equal(t, "same\n", buf.String())

	// Next batch starts at ts again, so it contains a's entry again plus b's.
	buf.Reset()
	printed, _, err := p.PrintResult(loghttp.Streams{
		{Labels: streamA, Entries: []loghttp.Entry{{Timestamp: ts, Line: "same"}}},
		{Labels: streamB, Entries: []loghttp.Entry{{Timestamp: ts, Line: "same"}}},
	}, out, last)
	require.NoError(t, err)
	require.Equal(t, 1, printed, "only stream b's entry is new")
	require.Equal(t, "same\n", buf.String())
}
