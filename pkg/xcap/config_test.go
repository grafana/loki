package xcap

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilteringConfig_Validate_StrategyName(t *testing.T) {
	tests := []struct {
		name         string
		strategyName DropStrategyName
		expectErr    bool
	}{
		{
			name:         "empty defaults to prune_leaves",
			strategyName: "",
			expectErr:    false,
		},
		{
			name:         "prune_leaves is valid",
			strategyName: DropStrategyPruneLeaves,
			expectErr:    false,
		},
		{
			name:         "promote_children is valid",
			strategyName: DropStrategyPromoteChildren,
			expectErr:    false,
		},
		{
			name:         "drop_subtree is valid",
			strategyName: DropStrategyDropSubtree,
			expectErr:    false,
		},
		{
			name:         "unknown strategy returns error",
			strategyName: "unknown_strategy",
			expectErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := FilteringConfig{
				MinRegionDuration: 1 * time.Millisecond,
				DropStrategyName:  tt.strategyName,
			}
			err := cfg.Validate()
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFilteringConfig_resolveStrategy(t *testing.T) {
	tests := []struct {
		name         string
		cfg          FilteringConfig
		expectedName string
	}{
		{
			name: "disabled returns noop",
			cfg: FilteringConfig{
				MinRegionDuration: 0,
				DropStrategyName:  DropStrategyPruneLeaves,
			},
			expectedName: "noop",
		},
		{
			name: "negative duration returns noop",
			cfg: FilteringConfig{
				MinRegionDuration: -1 * time.Millisecond,
				DropStrategyName:  DropStrategyPruneLeaves,
			},
			expectedName: "noop",
		},
		{
			name: "empty strategy defaults to prune_leaves",
			cfg: FilteringConfig{
				MinRegionDuration: time.Millisecond,
				// DropStrategyName not set
			},
			expectedName: "prune_leaves",
		},
		{
			name: "prune_leaves",
			cfg: FilteringConfig{
				MinRegionDuration: time.Millisecond,
				DropStrategyName:  DropStrategyPruneLeaves,
			},
			expectedName: "prune_leaves",
		},
		{
			name: "promote_children",
			cfg: FilteringConfig{
				MinRegionDuration: time.Millisecond,
				DropStrategyName:  DropStrategyPromoteChildren,
			},
			expectedName: "promote_children",
		},
		{
			name: "drop_subtree",
			cfg: FilteringConfig{
				MinRegionDuration: time.Millisecond,
				DropStrategyName:  DropStrategyDropSubtree,
			},
			expectedName: "drop_subtree",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy, err := tt.cfg.resolveStrategy()
			require.NoError(t, err)
			require.NotNil(t, strategy)
			assert.Equal(t, tt.expectedName, strategy.Name())
		})
	}
}
