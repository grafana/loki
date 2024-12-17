package print

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

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
