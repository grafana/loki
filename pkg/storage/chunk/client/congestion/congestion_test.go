package congestion

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

func TestZeroValueConstruction(t *testing.T) {
	cfg := Config{}
	m := NewMetrics(t.Name(), cfg)
	ctrl := NewController(cfg, log.NewNopLogger(), m)

	require.IsType(t, &NoopController{}, ctrl)
	require.IsType(t, &NoopRetrier{}, ctrl.getRetrier())
	require.IsType(t, &NoopHedger{}, ctrl.getHedger())
	m.Unregister()
}

func TestAIMDConstruction(t *testing.T) {
	cfg := Config{
		Controller: ControllerConfig{
			Strategy: "aimd",
		},
	}
	m := NewMetrics(t.Name(), cfg)
	ctrl := NewController(cfg, log.NewNopLogger(), m)

	require.IsType(t, &AIMDController{}, ctrl)
	require.IsType(t, &NoopRetrier{}, ctrl.getRetrier())
	require.IsType(t, &NoopHedger{}, ctrl.getHedger())
	m.Unregister()
}

func TestRetrierConstruction(t *testing.T) {
	cfg := Config{
		Retry: RetrierConfig{
			Strategy: "limited",
		},
	}
	m := NewMetrics(t.Name(), cfg)
	ctrl := NewController(cfg, log.NewNopLogger(), m)

	require.IsType(t, &NoopController{}, ctrl)
	require.IsType(t, &LimitedRetrier{}, ctrl.getRetrier())
	require.IsType(t, &NoopHedger{}, ctrl.getHedger())
	m.Unregister()
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
	m := NewMetrics(t.Name(), cfg)
	ctrl := NewController(cfg, log.NewNopLogger(), m)

	require.IsType(t, &AIMDController{}, ctrl)
	require.IsType(t, &LimitedRetrier{}, ctrl.getRetrier())
	require.IsType(t, &NoopHedger{}, ctrl.getHedger())
	m.Unregister()
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
