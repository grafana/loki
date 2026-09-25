package queryfrontend

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"

	loglineconfig "github.com/grafana/loki/v3/pkg/logline/config"
)

func TestRegisterFlagsDoesNotRedefineStore(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var lokiCfg loglineconfig.Config
	var qfCfg Config

	lokiCfg.RegisterFlags(fs)
	require.NotPanics(t, func() { qfCfg.RegisterFlags(fs) })

	require.NotNil(t, fs.Lookup("logline-store.bucket-prefix"))
	require.NotNil(t, fs.Lookup("logline-store.min-date"))
	require.NotNil(t, fs.Lookup("logline.enabled"))
	require.NotNil(t, fs.Lookup("logline-query-frontend.dry-run"))
	require.NotNil(t, fs.Lookup("logline-query-frontend.max-hint-parallel"))
	require.NotNil(t, fs.Lookup("logline-query-frontend.min-query-bytes-for-index"))
	require.NotNil(t, fs.Lookup("logline-query-frontend.require-opt-in-header"))
}
