package loki

import (
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
)

func Test_calculateMaxLookBack(t *testing.T) {
	type args struct {
		pc                chunk.PeriodConfig
		maxLookBackConfig time.Duration
		maxChunkAge       time.Duration
	}
	tests := []struct {
		name    string
		args    args
		want    time.Duration
		wantErr bool
	}{
		{
			name: "default",
			args: args{
				pc: chunk.PeriodConfig{
					ObjectType: "filesystem",
				},
				maxLookBackConfig: 0,
				maxChunkAge:       1 * time.Hour,
			},
			want:    90 * time.Minute,
			wantErr: false,
		},
		{
			name: "infinite",
			args: args{
				pc: chunk.PeriodConfig{
					ObjectType: "filesystem",
				},
				maxLookBackConfig: -1,
				maxChunkAge:       1 * time.Hour,
			},
			want:    -1,
			wantErr: false,
		},
		{
			name: "invalid store type",
			args: args{
				pc: chunk.PeriodConfig{
					ObjectType: "gcs",
				},
				maxLookBackConfig: -1,
				maxChunkAge:       1 * time.Hour,
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "less than default",
			args: args{
				pc: chunk.PeriodConfig{
					ObjectType: "filesystem",
				},
				maxLookBackConfig: 1 * time.Hour,
				maxChunkAge:       1 * time.Hour,
			},
			want:    0,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calculateMaxLookBack(tt.args.pc, tt.args.maxLookBackConfig, tt.args.maxChunkAge)
			if (err != nil) != tt.wantErr {
				t.Errorf("calculateMaxLookBack() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("calculateMaxLookBack() got = %v, want %v", got, tt.want)
			}
		})
	}
}
