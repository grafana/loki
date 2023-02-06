package gcplog

import (
	"testing"
)

func TestConvertToLokiCompatibleLabel(t *testing.T) {
	type args struct {
		label string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Google timestamp label attribute name",
			args: args{
				label: "logging.googleapis.com/timestamp",
			},
			want: "logging_googleapis_com_timestamp",
		},
		{
			name: "Label attribute name with multiple non-underscore characters",
			args: args{
				label: "logging.googleapis.com/Crazy-label",
			},
			want: "logging_googleapis_com_crazy_label",
		},
		{
			name: "Label attribute name in CamelCase converted into SnakeCase",
			args: args{
				label: "logging.googleapis.com/CrazyLabel",
			},
			want: "logging_googleapis_com_crazy_label",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := convertToLokiCompatibleLabel(tt.args.label); got != tt.want {
				t.Errorf("convertToLokiCompatibleLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}
