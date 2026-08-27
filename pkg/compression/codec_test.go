package compression

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodecs(t *testing.T) {
	got := Codecs()
	require.Equal(t, supportedCodecs, got)

	got[0] = Zstd
	require.Equal(t, None, supportedCodecs[0], "mutating the returned slice must not affect supportedCodecs")
}

func TestParseEncoding(t *testing.T) {
	tests := []struct {
		enc     string
		want    Codec
		wantErr bool
	}{
		{"gzip", GZIP, false},
		{"bad", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.enc, func(t *testing.T) {
			got, err := ParseCodec(tt.enc)
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
