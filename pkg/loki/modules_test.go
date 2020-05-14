package loki

import (
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

func TestOrderedDeps(t *testing.T) {
	for _, m := range []moduleName{All, Distributor, Ingester, Querier} {
		deps := orderedDeps(m)
		seen := make(map[moduleName]struct{})
		// make sure that getDeps always orders dependencies correctly.
		for _, d := range deps {
			seen[d] = struct{}{}
			for _, dep := range modules[d].deps {
				if _, ok := seen[dep]; !ok {
					t.Errorf("module %s has dependency %s which has not been seen.", d, dep)
				}
			}
		}
	}
}

func TestOrderedDepsShouldGuaranteeStabilityAcrossMultipleRuns(t *testing.T) {
	initial := orderedDeps(All)

	for i := 0; i < 10; i++ {
		assert.Equal(t, initial, orderedDeps(All))
	}
}

func TestUniqueDeps(t *testing.T) {
	input := []moduleName{Server, Overrides, Distributor, Overrides, Server, Ingester, Server}
	expected := []moduleName{Server, Overrides, Distributor, Ingester}
	assert.Equal(t, expected, uniqueDeps(input))
}

func TestActiveIndexType(t *testing.T) {
	var cfg chunk.SchemaConfig

	// just one PeriodConfig in the past
	cfg.Configs = []chunk.PeriodConfig{{
		From:      chunk.DayTime{Time: model.Now().Add(-24 * time.Hour)},
		IndexType: "first",
	}}

	assert.Equal(t, cfg.Configs[0], activePeriodConfig(cfg))

	// add a newer PeriodConfig in the past which should be considered
	cfg.Configs = append(cfg.Configs, chunk.PeriodConfig{
		From:      chunk.DayTime{Time: model.Now().Add(-12 * time.Hour)},
		IndexType: "second",
	})
	assert.Equal(t, cfg.Configs[1], activePeriodConfig(cfg))

	// add a newer PeriodConfig in the future which should not be considered
	cfg.Configs = append(cfg.Configs, chunk.PeriodConfig{
		From:      chunk.DayTime{Time: model.Now().Add(time.Hour)},
		IndexType: "third",
	})
	assert.Equal(t, cfg.Configs[1], activePeriodConfig(cfg))

}

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
