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

// This test guards the Controller.Strategy term of Config.ReplacesInnerRetries. Without
// that term, the storage factory disables the retries of the object-store client behind
// a pass-through controller.
func TestNoopControllerWrapIsPassThrough(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Retry: RetrierConfig{
			Strategy: "limited",
			Limit:    2,
		},
	}
	m := NewMetrics(t.Name(), cfg)
	t.Cleanup(m.Unregister)

	ctrl := NewController(cfg, log.NewNopLogger(), m)
	require.IsType(t, &NoopController{}, ctrl)
	require.IsType(t, &LimitedRetrier{}, ctrl.getRetrier())

	inner := newMockObjectClient(maxFailer{max: 0})
	require.Same(t, inner, ctrl.Wrap(inner), "NoopController.Wrap must return the inner client unwrapped")

	require.False(t, cfg.ReplacesInnerRetries("s3"), "a pass-through controller does not replace the inner client's retries")
}

func TestReplacesInnerRetries_StoreType(t *testing.T) {
	cfg := Config{
		Enabled:    true,
		Controller: ControllerConfig{Strategy: StrategyAIMD},
		Retry:      RetrierConfig{Strategy: RetryStrategyLimited, Limit: 2},
	}
	require.True(t, cfg.ReplacesInnerRetries("s3"))
	require.True(t, cfg.ReplacesInnerRetries("gcs"))
	require.False(t, cfg.ReplacesInnerRetries("azure"))
	require.False(t, cfg.ReplacesInnerRetries("swift"))
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
