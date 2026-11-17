package logql

import (
	"context"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"github.com/grafana/loki/v3/pkg/logproto"
)

const histogramFlagName = "querier.engine.enable-histograms"

func TestEngineOpts_EnableHistogramsConfig(t *testing.T) {
	t.Run("the flag is off by default and is set with the CLI flag", func(t *testing.T) {
		var opts EngineOpts
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		opts.RegisterFlagsWithPrefix("querier.engine.", fs)
		require.False(t, opts.EnableHistograms)

		require.NoError(t, fs.Parse([]string{"-" + histogramFlagName + "=true"}))
		require.True(t, opts.EnableHistograms)
	})

	t.Run("the YAML key", func(t *testing.T) {
		var opts EngineOpts
		// Strict, as the loading of the configuration is, so that a key that is not known is an error.
		dec := yaml.NewDecoder(strings.NewReader("enable_histograms: true\n"))
		dec.KnownFields(true)
		require.NoError(t, dec.Decode(&opts))
		require.True(t, opts.EnableHistograms)
	})

	t.Run("the flag is documented as experimental", func(t *testing.T) {
		var opts EngineOpts
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		opts.RegisterFlagsWithPrefix("querier.engine.", fs)
		f := fs.Lookup(histogramFlagName)
		require.NotNil(t, f)
		require.Equal(t, "false", f.DefValue)
		require.Contains(t, f.Usage, "Experimental")
	})
}

// A query gets the options of the engine that runs it. The downstream engine of the query frontend builds
// them on its own, and it must not leave one out: that would ignore the setting of a frontend without a
// sign of it.
func TestEngine_QueryOpts(t *testing.T) {
	opts := EngineOpts{EnableHistograms: true, MaxQuerySeriesCutoff: true, LogExecutingQuery: true}
	params, err := NewLiteralParams(`count_over_time({app="a"}[1m])`, time.Unix(60, 0), time.Unix(60, 0), 0, 0, logproto.FORWARD, 100, nil, nil)
	require.NoError(t, err)

	t.Run("the engine", func(t *testing.T) {
		eng := NewEngine(opts, NewMockQuerier(0, nil), NoLimits, log.NewNopLogger())

		q, ok := eng.Query(params).(*query)
		require.True(t, ok)
		require.Equal(t, queryOpts{logExecutingQuery: true, maxQuerySeriesCutoff: true, histogramsEnabled: true}, q.opts)
	})

	t.Run("the downstream engine", func(t *testing.T) {
		regular := NewEngine(opts, NewMockQuerier(0, nil), NoLimits, log.NewNopLogger())
		eng := NewDownstreamEngine(opts, MockDownstreamer{regular}, NoLimits, log.NewNopLogger())

		q, ok := eng.Query(context.Background(), params).(*query)
		require.True(t, ok)
		// The query that it downstreams is logged by the engine that issued it.
		require.Equal(t, queryOpts{logExecutingQuery: false, maxQuerySeriesCutoff: true, histogramsEnabled: true}, q.opts)
	})
}
