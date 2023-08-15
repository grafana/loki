package congestion

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZeroValueConstruction(t *testing.T) {
	cfg := Config{}
	ctrl := NewController(cfg, NewMetrics(t.Name(), cfg))

	require.IsType(t, &NoopController{}, ctrl)
	require.IsType(t, &NoopRetrier{}, ctrl.getRetrier())
	require.IsType(t, &NoopHedger{}, ctrl.getHedger())
}

func TestAIMDConstruction(t *testing.T) {
	cfg := Config{
		Controller: ControllerConfig{
			Strategy: "aimd",
		},
	}
	ctrl := NewController(cfg, NewMetrics(t.Name(), cfg))

	require.IsType(t, &AIMDController{}, ctrl)
	require.IsType(t, &NoopRetrier{}, ctrl.getRetrier())
	require.IsType(t, &NoopHedger{}, ctrl.getHedger())
}

func TestRetrierConstruction(t *testing.T) {
	cfg := Config{
		Retry: RetrierConfig{
			Strategy: "limited",
		},
	}
	ctrl := NewController(cfg, NewMetrics(t.Name(), cfg))

	require.IsType(t, &NoopController{}, ctrl)
	require.IsType(t, &LimitedRetrier{}, ctrl.getRetrier())
	require.IsType(t, &NoopHedger{}, ctrl.getHedger())
}

func TestCombinedConstruction(t *testing.T) {
	cfg := Config{
		Controller: ControllerConfig{
			Strategy: "aimd",
		},
		Retry: RetrierConfig{
			Strategy: "limited",
		},
	}
	ctrl := NewController(cfg, NewMetrics(t.Name(), cfg))

	require.IsType(t, &AIMDController{}, ctrl)
	require.IsType(t, &LimitedRetrier{}, ctrl.getRetrier())
	require.IsType(t, &NoopHedger{}, ctrl.getHedger())
}

func TestHedgerConstruction(t *testing.T) {
	//cfg := Config{
	//	Hedge: HedgerConfig{
	//		Strategy: "dont-hedge-retries",
	//	},
	//}
	// TODO(dannyk): implement hedging
	t.Skip("hedging not yet implemented")
}
