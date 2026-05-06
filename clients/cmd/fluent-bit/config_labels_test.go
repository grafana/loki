package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logqlmodel"

	lokiflag "github.com/grafana/loki/v3/pkg/util/flagext"
)

func Test_externalLabelsFromFluentBitLabelsOption(t *testing.T) {
	tests := []struct {
		name                 string
		labels               string
		want                 lokiflag.LabelSet
		wantErr              bool
		errContain           string
		wantErrParseMatchers bool
	}{
		{
			name:   "empty uses default job",
			labels: "",
			want:   lokiflag.LabelSet{LabelSet: model.LabelSet{"job": "fluent-bit"}},
		},
		{
			name:   "explicit default selector",
			labels: `{job="fluent-bit"}`,
			want:   lokiflag.LabelSet{LabelSet: model.LabelSet{"job": "fluent-bit"}},
		},
		{
			name:   "multiple equality matchers",
			labels: `{job="fluent-bit",env="prod"}`,
			want:   lokiflag.LabelSet{LabelSet: model.LabelSet{"job": "fluent-bit", "env": "prod"}},
		},
		{
			name:   "equality with regexp matcher",
			labels: `{job="app",level=~"info|warn"}`,
			want:   lokiflag.LabelSet{LabelSet: model.LabelSet{"job": "app", "level": "info|warn"}},
		},
		{
			name:   "mixed matchers from pkg/logql/syntax TestParseMatchers",
			labels: `{app!="foo",cluster=~".+bar",bar!~".?boo"}`,
			want: lokiflag.LabelSet{LabelSet: model.LabelSet{
				"app":     "foo",
				"cluster": ".+bar",
				"bar":     ".?boo",
			}},
		},
		{
			name:    "invalid token",
			labels:  "a",
			wantErr: true,
		},
		{
			name:    "unclosed brace",
			labels:  `{app="foo"`,
			wantErr: true,
		},
		{
			name:                 "line filter not allowed for ParseMatchers",
			labels:               `{app!="foo",cluster=~".+bar",bar!~".?boo"} |= "test"`,
			wantErr:              true,
			wantErrParseMatchers: true,
		},
		{
			name:       "only empty-compatible regexp matcher rejected by validation",
			labels:     `{foo=~".*"}`,
			wantErr:    true,
			errContain: "at least one regexp or equality matcher",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := externalLabelsFromFluentBitLabelsOption(tt.labels)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Fatalf("error %q should contain %q", err.Error(), tt.errContain)
				}
				if tt.wantErrParseMatchers && !errors.Is(err, logqlmodel.ErrParseMatchers) {
					t.Fatalf("expected ErrParseMatchers, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.LabelSet.Equal(tt.want.LabelSet) {
				t.Errorf("LabelSet = %v, want %v", got.LabelSet, tt.want.LabelSet)
			}
		})
	}
}
