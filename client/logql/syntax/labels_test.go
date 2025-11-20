package syntax

import (
	"testing"
)

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Labels
		wantErr bool
	}{
		{
			name:    "empty",
			input:   "{}",
			want:    EmptyLabels(),
			wantErr: false,
		},
		{
			name:    "single label",
			input:   `{job="test"}`,
			want:    Labels{{Name: "job", Value: "test"}},
			wantErr: false,
		},
		{
			name:    "multiple labels",
			input:   `{job="test", instance="localhost"}`,
			want:    Labels{{Name: "job", Value: "test"}, {Name: "instance", Value: "localhost"}},
			wantErr: false,
		},
		{
			name:    "no braces",
			input:   `job="test"`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty value",
			input:   `{job=""}`,
			want:    Labels{{Name: "job", Value: ""}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLabels(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLabels() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("ParseLabels() got %d labels, want %d", len(got), len(tt.want))
					return
				}
				// Check that all expected labels are present (order may vary)
				for _, wantLabel := range tt.want {
					found := false
					for _, gotLabel := range got {
						if gotLabel.Name == wantLabel.Name && gotLabel.Value == wantLabel.Value {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("ParseLabels() missing label %v", wantLabel)
					}
				}
			}
		})
	}
}

func TestLabels_Get(t *testing.T) {
	labels := Labels{
		{Name: "job", Value: "test"},
		{Name: "instance", Value: "localhost"},
	}

	if got := labels.Get("job"); got != "test" {
		t.Errorf("Labels.Get() = %v, want %v", got, "test")
	}

	if got := labels.Get("nonexistent"); got != "" {
		t.Errorf("Labels.Get() = %v, want %v", got, "")
	}
}

func TestLabels_Has(t *testing.T) {
	labels := Labels{
		{Name: "job", Value: "test"},
	}

	if !labels.Has("job") {
		t.Error("Labels.Has() = false, want true")
	}

	if labels.Has("nonexistent") {
		t.Error("Labels.Has() = true, want false")
	}
}
