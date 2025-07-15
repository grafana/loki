package partitionring

import "testing"

func TestExtractIngesterPartitionID(t *testing.T) {
	tests := []struct {
		name       string
		ingesterID string
		want       int32
		wantErr    bool
	}{
		{
			name:       "Valid ingester ID",
			ingesterID: "ingester-5",
			want:       5,
			wantErr:    false,
		},
		{
			name:       "Local ingester ID",
			ingesterID: "ingester-local",
			want:       0,
			wantErr:    false,
		},
		{
			name:       "Invalid ingester ID format",
			ingesterID: "invalid-format",
			want:       0,
			wantErr:    true,
		},
		{
			name:       "Invalid sequence number",
			ingesterID: "ingester-abc",
			want:       0,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractIngesterPartitionID(tt.ingesterID)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractIngesterPartitionID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractIngesterPartitionID() = %v, want %v", got, tt.want)
			}
		})
	}
}
