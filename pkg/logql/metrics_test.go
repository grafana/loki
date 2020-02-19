package logql

import (
	"testing"
)

func TestQueryType(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    string
		wantErr bool
	}{
		{"bad", "ddd", "", true},
		{"limited", `{app="foo"}`, typeLimited, false},
		{"limited multi label", `{app="foo" ,fuzz=~"foo"}`, typeLimited, false},
		{"filter", `{app="foo"} |= "foo"`, typeFilter, false},
		{"metrics", `rate({app="foo"} |= "foo"[5m])`, typeMetric, false},
		{"metrics binary", `rate({app="foo"} |= "foo"[5m]) + count_over_time({app="foo"} |= "foo"[5m]) / rate({app="foo"} |= "foo"[5m]) `, typeMetric, false},
		{"filters", `{app="foo"} |= "foo" |= "f" != "b"`, typeFilter, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := QueryType(tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("QueryType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("QueryType() = %v, want %v", got, tt.want)
			}
		})
	}
}
