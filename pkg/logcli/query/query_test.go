package query

import (
	"log"
	"reflect"
	"testing"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql/marshal"
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
				[]loghttp.LabelSet{mustParseLabels(`{foo="bar", bar="foo"}`), mustParseLabels(`{bar="foo", foo="foo", baz="baz"}`)},
			},
			mustParseLabels(`{bar="foo"}`),
		},
		{
			"Extract common labels source > target",
			args{
				[]loghttp.LabelSet{mustParseLabels(`{foo="bar", bar="foo"}`), mustParseLabels(`{bar="foo", foo="bar", baz="baz"}`)},
			},
			mustParseLabels(`{foo="bar", bar="foo"}`),
		},
		{
			"Extract common labels source < target",
			args{
				[]loghttp.LabelSet{mustParseLabels(`{foo="bar", bar="foo"}`), mustParseLabels(`{bar="foo"}`)},
			},
			mustParseLabels(`{bar="foo"}`),
		},
		{
			"Extract common labels source < target no common",
			args{
				[]loghttp.LabelSet{mustParseLabels(`{foo="bar", bar="foo"}`), mustParseLabels(`{fo="bar"}`)},
			},
			loghttp.LabelSet{},
		},
		{
			"Extract common labels source = target no common",
			args{
				[]loghttp.LabelSet{mustParseLabels(`{foo="bar"}`), mustParseLabels(`{fooo="bar"}`)},
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
				mustParseLabels(`{foo="bar", bar="foo"}`),
				mustParseLabels(`{bar="foo", foo="foo", baz="baz"}`),
			},
			mustParseLabels(`{foo="bar"}`),
		},
		{
			"Subtract labels source < target",
			args{
				mustParseLabels(`{foo="bar", bar="foo"}`),
				mustParseLabels(`{bar="foo"}`),
			},
			mustParseLabels(`{foo="bar"}`),
		},
		{
			"Subtract labels source < target no sub",
			args{
				mustParseLabels(`{foo="bar", bar="foo"}`),
				mustParseLabels(`{fo="bar"}`),
			},
			mustParseLabels(`{bar="foo", foo="bar"}`),
		},
		{
			"Subtract labels source = target no sub",
			args{
				mustParseLabels(`{foo="bar"}`),
				mustParseLabels(`{fiz="buz"}`),
			},
			mustParseLabels(`{foo="bar"}`),
		},
		{
			"Subtract labels source > target no sub",
			args{
				mustParseLabels(`{foo="bar"}`),
				mustParseLabels(`{fiz="buz", foo="baz"}`),
			},
			mustParseLabels(`{foo="bar"}`),
		},
		{
			"Subtract labels source > target no sub",
			args{
				mustParseLabels(`{a="b", foo="bar", baz="baz", fizz="fizz"}`),
				mustParseLabels(`{foo="bar", baz="baz", buzz="buzz", fizz="fizz"}`),
			},
			mustParseLabels(`{a="b"}`),
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

func mustParseLabels(s string) loghttp.LabelSet {
	l, err := marshal.NewLabelSet(s)

	if err != nil {
		log.Fatalf("Failed to parse %s", s)
	}

	return l
}
