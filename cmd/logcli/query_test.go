package main

import (
	"reflect"
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
)

func Test_commonLabels(t *testing.T) {
	type args struct {
		lss []labels.Labels
	}
	tests := []struct {
		name string
		args args
		want labels.Labels
	}{
		{
			"Extract common labels source > target",
			args{
				[]labels.Labels{mustParseLabels(`{foo="bar", bar="foo"}`), mustParseLabels(`{bar="foo", foo="foo", baz="baz"}`)},
			},
			mustParseLabels(`{bar="foo"}`),
		},
		{
			"Extract common labels source > target",
			args{
				[]labels.Labels{mustParseLabels(`{foo="bar", bar="foo"}`), mustParseLabels(`{bar="foo", foo="bar", baz="baz"}`)},
			},
			mustParseLabels(`{foo="bar", bar="foo"}`),
		},
		{
			"Extract common labels source < target",
			args{
				[]labels.Labels{mustParseLabels(`{foo="bar", bar="foo"}`), mustParseLabels(`{bar="foo"}`)},
			},
			mustParseLabels(`{bar="foo"}`),
		},
		{
			"Extract common labels source < target no common",
			args{
				[]labels.Labels{mustParseLabels(`{foo="bar", bar="foo"}`), mustParseLabels(`{fo="bar"}`)},
			},
			labels.Labels{},
		},
		{
			"Extract common labels source = target no common",
			args{
				[]labels.Labels{mustParseLabels(`{foo="bar"}`), mustParseLabels(`{fooo="bar"}`)},
			},
			labels.Labels{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commonLabels(tt.args.lss); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("commonLabels() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_subtract(t *testing.T) {
	type args struct {
		a labels.Labels
		b labels.Labels
	}
	tests := []struct {
		name string
		args args
		want labels.Labels
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
