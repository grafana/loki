package log

import "testing"

func Test_sanitizeLabelKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"1", "_1"},
		{"1 1 1", "_1_1_1"},
		{"abc", "abc"},
		{"$a$bc", "_a_bc"},
		{"$a$bc", "_a_bc"},
		{"   1 1 1  \t", "_1_1_1"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := sanitizeLabelKey(tt.key, true); got != tt.want {
				t.Errorf("sanitizeKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
