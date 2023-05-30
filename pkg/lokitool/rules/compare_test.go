package rules

import (
	"testing"

	"github.com/prometheus/prometheus/model/rulefmt"
	yaml "gopkg.in/yaml.v3"
)

func Test_rulesEqual(t *testing.T) {
	tests := []struct {
		name string
		a    *rulefmt.RuleNode
		b    *rulefmt.RuleNode
		want bool
	}{
		{
			name: "rule_node_identical",
			a: &rulefmt.RuleNode{
				Record:      yaml.Node{Value: "one"},
				Expr:        yaml.Node{Value: "up"},
				Annotations: map[string]string{"a": "b", "c": "d"},
				Labels:      nil,
			},
			b: &rulefmt.RuleNode{
				Record:      yaml.Node{Value: "one"},
				Expr:        yaml.Node{Value: "up"},
				Annotations: map[string]string{"c": "d", "a": "b"},
				Labels:      nil,
			},
			want: true,
		},
		{
			name: "rule_node_diff",
			a: &rulefmt.RuleNode{
				Record: yaml.Node{Value: "one"},
				Expr:   yaml.Node{Value: "up"},
			},
			b: &rulefmt.RuleNode{
				Record: yaml.Node{Value: "two"},
				Expr:   yaml.Node{Value: "up"},
			},
			want: false,
		},
		{
			name: "rule_node_annotations_diff",
			a: &rulefmt.RuleNode{
				Record:      yaml.Node{Value: "one"},
				Expr:        yaml.Node{Value: "up"},
				Annotations: map[string]string{"a": "b"},
			},
			b: &rulefmt.RuleNode{
				Record:      yaml.Node{Value: "one", Column: 10},
				Expr:        yaml.Node{Value: "up"},
				Annotations: map[string]string{"c": "d"},
			},
			want: false,
		},
		{
			name: "rule_node_annotations_nil_diff",
			a: &rulefmt.RuleNode{
				Record:      yaml.Node{Value: "one"},
				Expr:        yaml.Node{Value: "up"},
				Annotations: map[string]string{"a": "b"},
			},
			b: &rulefmt.RuleNode{
				Record:      yaml.Node{Value: "one", Column: 10},
				Expr:        yaml.Node{Value: "up"},
				Annotations: nil,
			},
			want: false,
		},
		{
			name: "rule_node_yaml_diff",
			a: &rulefmt.RuleNode{
				Record: yaml.Node{Value: "one"},
				Expr:   yaml.Node{Value: "up"},
			},
			b: &rulefmt.RuleNode{
				Record: yaml.Node{Value: "one", Column: 10},
				Expr:   yaml.Node{Value: "up"},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rulesEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("rulesEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompareGroups(t *testing.T) {
	tests := []struct {
		name        string
		groupOne    rulefmt.RuleGroup
		groupTwo    rulefmt.RuleGroup
		expectedErr error
	}{
		{
			name: "identical configs",
			groupOne: rulefmt.RuleGroup{
				Name: "example_group",
				Rules: []rulefmt.RuleNode{
					{
						Record:      yaml.Node{Value: "one"},
						Expr:        yaml.Node{Value: "up"},
						Annotations: map[string]string{"a": "b", "c": "d"},
						Labels:      nil,
					},
				},
			},
			groupTwo: rulefmt.RuleGroup{
				Name: "example_group",
				Rules: []rulefmt.RuleNode{
					{
						Record:      yaml.Node{Value: "one"},
						Expr:        yaml.Node{Value: "up"},
						Annotations: map[string]string{"a": "b", "c": "d"},
						Labels:      nil,
					},
				},
			},
			expectedErr: nil,
		},
		{
			name: "different rule length",
			groupOne: rulefmt.RuleGroup{
				Name: "example_group",
				Rules: []rulefmt.RuleNode{
					{
						Record:      yaml.Node{Value: "one"},
						Expr:        yaml.Node{Value: "up"},
						Annotations: map[string]string{"a": "b", "c": "d"},
						Labels:      nil,
					},
				},
			},
			groupTwo: rulefmt.RuleGroup{
				Name: "example_group",
				Rules: []rulefmt.RuleNode{
					{
						Record:      yaml.Node{Value: "one"},
						Expr:        yaml.Node{Value: "up"},
						Annotations: map[string]string{"a": "b", "c": "d"},
						Labels:      nil,
					},
					{
						Record:      yaml.Node{Value: "one"},
						Expr:        yaml.Node{Value: "up"},
						Annotations: map[string]string{"a": "b", "c": "d"},
						Labels:      nil,
					},
				},
			},
			expectedErr: errDiffRuleLen,
		},
		{
			name: "identical rw configs",
			groupOne: rulefmt.RuleGroup{
				Name: "example_group",
				Rules: []rulefmt.RuleNode{
					{
						Record:      yaml.Node{Value: "one"},
						Expr:        yaml.Node{Value: "up"},
						Annotations: map[string]string{"a": "b", "c": "d"},
						Labels:      nil,
					},
				},
			},
			groupTwo: rulefmt.RuleGroup{
				Name: "example_group",
				Rules: []rulefmt.RuleNode{
					{
						Record:      yaml.Node{Value: "one"},
						Expr:        yaml.Node{Value: "up"},
						Annotations: map[string]string{"a": "b", "c": "d"},
						Labels:      nil,
					},
				},
			},
			expectedErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CompareGroups(tt.groupOne, tt.groupTwo); err != nil {
				if err != tt.expectedErr {
					t.Errorf("CompareGroups() error = %v, wantErr %v", err, tt.expectedErr)
				}
			}
		})
	}
}
