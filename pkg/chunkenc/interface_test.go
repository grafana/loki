package chunkenc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseEncoding(t *testing.T) {
	tests := []struct {
		enc     string
		want    Encoding
		wantErr bool
	}{
		{"gzip", EncGZIP, false},
		{"bad", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.enc, func(t *testing.T) {
			got, err := ParseEncoding(tt.enc)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseEncoding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseEncoding() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsOutOfOrderErr(t *testing.T) {
	now := time.Now()

	for _, err := range []error{ErrOutOfOrder, ErrTooFarBehind(now, now)} {
		require.Equal(t, true, IsOutOfOrderErr(err))
	}
}
