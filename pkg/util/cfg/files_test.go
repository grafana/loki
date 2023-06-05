package cfg

import (
	"flag"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
)

type testCfg struct {
	v int
}

func (cfg *testCfg) RegisterFlags(f *flag.FlagSet) {
	cfg.v++
}

func (cfg *testCfg) Clone() flagext.Registerer {
	return func(cfg testCfg) flagext.Registerer {
		return &cfg
	}(*cfg)
}

func TestConfigFileLoaderDoesNotMutate(t *testing.T) {
	cfg := &testCfg{}
	err := ConfigFileLoader(nil, "something", true, false)(cfg)
	require.Nil(t, err)
	require.Equal(t, 0, cfg.v)

	cfg.RegisterFlags(nil)
	require.Equal(t, 1, cfg.v)
}
