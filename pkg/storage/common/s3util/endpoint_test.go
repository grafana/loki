package s3util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatEndpoint(t *testing.T) {
	tests := []struct {
		name             string
		endpoint         string
		insecure         bool
		expectedEndpoint string
	}{
		{
			name:             "endpoint without scheme and insecure=false should add https",
			endpoint:         "s3.example.com",
			insecure:         false,
			expectedEndpoint: "https://s3.example.com",
		},
		{
			name:             "endpoint without scheme and insecure=true should add http",
			endpoint:         "s3.example.com",
			insecure:         true,
			expectedEndpoint: "http://s3.example.com",
		},
		{
			name:             "endpoint with https scheme should remain unchanged",
			endpoint:         "https://s3.example.com",
			insecure:         false,
			expectedEndpoint: "https://s3.example.com",
		},
		{
			name:             "endpoint with http scheme should remain unchanged",
			endpoint:         "http://s3.example.com",
			insecure:         true,
			expectedEndpoint: "http://s3.example.com",
		},
		{
			name:             "endpoint with port and no scheme and insecure=false should add https",
			endpoint:         "s3.example.com:9000",
			insecure:         false,
			expectedEndpoint: "https://s3.example.com:9000",
		},
		{
			name:             "endpoint with port and no scheme and insecure=true should add http",
			endpoint:         "s3.example.com:9000",
			insecure:         true,
			expectedEndpoint: "http://s3.example.com:9000",
		},
		{
			name:             "empty endpoint should return empty string",
			endpoint:         "",
			insecure:         false,
			expectedEndpoint: "",
		},
		{
			name:             "empty endpoint with insecure=true should return empty string",
			endpoint:         "",
			insecure:         true,
			expectedEndpoint: "",
		},
		{
			name:             "endpoint with https scheme and insecure=true should remain unchanged",
			endpoint:         "https://s3.example.com",
			insecure:         true,
			expectedEndpoint: "https://s3.example.com",
		},
		{
			name:             "endpoint with http scheme and insecure=false should remain unchanged",
			endpoint:         "http://s3.example.com",
			insecure:         false,
			expectedEndpoint: "http://s3.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatEndpoint(tt.endpoint, tt.insecure)
			require.Equal(t, tt.expectedEndpoint, result)
		})
	}
}
