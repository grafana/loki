package chunkenc

import "testing"

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
