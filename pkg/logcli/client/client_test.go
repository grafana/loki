package client

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func Test_getHTTPRequestHeader(t *testing.T) {
	tests := []struct {
		name    string
		client  Client
		want    http.Header
		wantErr bool
	}{
		{"empty", &DefaultClient{}, http.Header{}, false},
		{"partial-headers", &DefaultClient{
			OrgID:     "124",
			QueryTags: "source=abc",
		}, http.Header{
			"X-Scope-OrgID": []string{"124"},
			"X-Query-Tags":  []string{"source=abc"},
		}, false},
		{"basic-auth", &DefaultClient{
			Username: "123",
			Password: "secure",
		}, http.Header{
			"Authorization": []string{"Basic " + base64.StdEncoding.EncodeToString([]byte("123:secure"))},
		}, false},
		{"bearer-token", &DefaultClient{
			BearerToken: "secureToken",
		}, http.Header{
			"Authorization": []string{"Bearer " + "secureToken"},
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaultClient := tt.client.(*DefaultClient)
			got, err := defaultClient.getHTTPRequestHeader(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("getHTTPRequestHeader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// User-Agent should be set all the time.
			assert.Equal(t, got["User-Agent"], []string{userAgent})

			for k := range tt.want {
				ck := http.CanonicalHeaderKey(k)
				assert.Equal(t, tt.want[k], got[ck])
			}
		})
	}
}
