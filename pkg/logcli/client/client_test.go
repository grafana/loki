package client

import "testing"

func Test_buildURL(t *testing.T) {
	tests := []struct {
		name    string
		u, p    string
		want    string
		wantErr bool
	}{
		{"err", "8://2", "/bar", "", true},
		{"strip /", "http://localhost//", "//bar", "http://localhost/bar", false},
		{"sub path", "https://localhost/loki/", "/bar/foo", "https://localhost/loki/bar/foo", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildURL(tt.u, tt.p)
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
