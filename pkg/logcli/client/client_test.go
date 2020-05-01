package client

import "testing"

func Test_buildURL(t *testing.T) {
	tests := []struct {
		name    string
		u, p, q string
		want    string
		wantErr bool
	}{
		{"err", "8://2", "/bar", "", "", true},
		{"strip /", "http://localhost//", "//bar", "a=b", "http://localhost/bar?a=b", false},
		{"sub path", "https://localhost/loki/", "/bar/foo", "c=d&e=f", "https://localhost/loki/bar/foo?c=d&e=f", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildURL(tt.u, tt.p, tt.q)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("buildURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
