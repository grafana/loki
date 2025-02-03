package rules

import (
	"fmt"
	"testing"

	"github.com/prometheus/prometheus/model/rulefmt"

	"github.com/grafana/loki/v3/pkg/tool/rules/rwrulefmt"
)

func TestParseFiles(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		want    map[string]RuleNamespace
		wantErr bool
	}{
		{
			name: "basic_loki_file",
			files: []string{
				"testdata/loki_basic.yaml",
			},
			want: map[string]RuleNamespace{
				"loki_basic": {
					Namespace: "loki_basic",
					Groups: []rwrulefmt.RuleGroup{
						{
							RuleGroup: rulefmt.RuleGroup{
								Name: "testgrp2",
								Rules: []rulefmt.RuleNode{
									{
										// currently the tests only check length
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "basic_loki_namespace",
			files: []string{
				"testdata/loki_basic_namespace.yaml",
			},
			want: map[string]RuleNamespace{
				"foo": {
					Namespace: "foo",
					Groups: []rwrulefmt.RuleGroup{
						{
							RuleGroup: rulefmt.RuleGroup{
								Name: "testgrp2",
								Rules: []rulefmt.RuleNode{
									{
										// currently the tests only check length
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "basic_loki_failure",
			files: []string{
				"testdata/loki_basic_failure.yaml",
			},
			wantErr: true,
		},
		{
			name: "multiple_loki_namespace",
			files: []string{
				"testdata/loki_multiple_namespace.yaml",
			},
			want: map[string]RuleNamespace{
				"foo": {
					Namespace: "foo",
					Groups: []rwrulefmt.RuleGroup{
						{
							RuleGroup: rulefmt.RuleGroup{
								Name: "testgrp2",
								Rules: []rulefmt.RuleNode{
									{
										// currently the tests only check length
									},
								},
							},
						},
					},
				},
				"other_foo": {
					Namespace: "other_foo",
					Groups: []rwrulefmt.RuleGroup{
						{
							RuleGroup: rulefmt.RuleGroup{
								Name: "other_testgrp2",
								Rules: []rulefmt.RuleNode{
									{
										// currently the tests only check length
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFiles(tt.files)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFiles() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			for k, g := range got {
				w, exists := tt.want[k]
				if !exists {
					t.Errorf("ParseFiles() namespace %v found and not expected", k)
					return
				}
				err = compareNamespace(g, w)
				if err != nil {
					t.Errorf("ParseFiles() namespaces do not match, err=%v", err)
					return
				}
			}
			for k := range tt.want {
				if _, exists := got[k]; !exists {
					t.Errorf("ParseFiles() namespace %v wanted but not found", k)
					return
				}
			}
		})
	}
}

func compareNamespace(g, w RuleNamespace) error {
	if g.Namespace != w.Namespace {
		return fmt.Errorf("namespaces do not match, actual=%v expected=%v", g.Namespace, w.Namespace)
	}

	if len(g.Groups) != len(w.Groups) {
		return fmt.Errorf("returned namespace does not have the expected number of groups, actual=%d expected=%d", len(g.Groups), len(w.Groups))
	}

	for i := range g.Groups {
		if g.Groups[i].Name != w.Groups[i].Name {
			return fmt.Errorf("actual group with name %v does not match expected group name %v", g.Groups[i].Name, w.Groups[i].Name)
		}
		if g.Groups[i].Interval != w.Groups[i].Interval {
			return fmt.Errorf("actual group with Interval %v does not match expected group Interval %v", g.Groups[i].Interval, w.Groups[i].Interval)
		}
		if len(g.Groups[i].Rules) != len(w.Groups[i].Rules) {
			return fmt.Errorf("length of rules do not match, actual=%v expected=%v", len(g.Groups[i].Rules), len(w.Groups[i].Rules))
		}
	}

	return nil
}
