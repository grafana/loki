package plugins

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"unsafe"

	gklog "github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/plugins/host"
	"github.com/grafana/loki/v3/pkg/plugins/metrics"
	"github.com/grafana/loki/v3/pkg/plugins/pushrequest"
	"github.com/grafana/loki/v3/pkg/runtime"
)

type Config struct {
	Enabled bool `yaml:"enabled" category:"experimental"`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, "distributor.database-plugins.enabled", false, "Enables database plugins.")
}

type Plugin struct {
	module   api.Module
	counters map[string]prometheus.Counter
}

type PluginMiddleware struct {
	runtime  wazero.Runtime
	plugins  []api.Module
	exchange *host.Exchange
}

func NewPluginMiddleware(ctx context.Context, reg prometheus.Registerer) (*PluginMiddleware, error) {
	tmp := os.TempDir() // todo use a more appropriate directory for the compilation cache
	cache, err := wazero.NewCompilationCacheWithDir(tmp)
	if err != nil {
		return nil, fmt.Errorf("failed to create compilation cache: %v", err)
	}

	config := wazero.NewRuntimeConfigCompiler().WithCompilationCache(cache)
	runtime := wazero.NewRuntimeWithConfig(ctx, config)
	wasi_snapshot_preview1.MustInstantiate(ctx, runtime)

	hostModuleBuilder := runtime.NewHostModuleBuilder("env")

	exchange := host.NewExchange()
	exchange.AddImports(hostModuleBuilder)

	metrics.AddImport(hostModuleBuilder, exchange, metrics.NewHostPluginMetrics(reg))
	pushrequest.AddImport(hostModuleBuilder, exchange, pushrequest.HostPluginPushRequest{Exchange: exchange})

	_, err = hostModuleBuilder.Instantiate(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to instantiate host module: %v", err)
	}

	return &PluginMiddleware{
		runtime:  runtime,
		exchange: exchange,
	}, nil
}

func (p *PluginMiddleware) HandlePushRequestWrapper(inner push.RequestParser) push.RequestParser {
	return func(userID string, r *http.Request, limits push.Limits, tenantConfigs *runtime.TenantConfigs, maxRecvMsgSize int, tracker push.UsageTracker, streamResolver push.StreamResolver, logger gklog.Logger) (*logproto.PushRequest, *push.Stats, error) {
		req, stats, err := inner(userID, r, limits, tenantConfigs, maxRecvMsgSize, tracker, streamResolver, logger)
		if err != nil {
			return req, stats, err
		}

		reqPtr := uint64(uintptr(unsafe.Pointer(req)))
		err = p.call(r.Context(), "process_push_request", reqPtr)
		if err != nil {
			level.Error(logger).Log("msg", "failed to process push request with plugin", "err", err)
		}
		return req, stats, nil
	}
}

func (p *PluginMiddleware) Register(ctx context.Context, pluginFile string) error {
	wasmBytes, err := os.ReadFile(pluginFile)
	if err != nil {
		return fmt.Errorf("failed to read WASM file: %w", err)
	}

	compiledModule, err := p.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("failed to compile WASM module: %w", err)
	}

	moduleCfg := wazero.NewModuleConfig().WithStdout(os.Stdout).WithStderr(os.Stderr).WithName(pluginFile)
	plugin, err := p.runtime.InstantiateModule(ctx, compiledModule, moduleCfg)
	if err != nil {
		return fmt.Errorf("failed to instantiate WASM module: %w", err)
	}

	if _, ok := plugin.ExportedFunctionDefinitions()["setup"]; ok {
		_, err = plugin.ExportedFunction("setup").Call(ctx)
		if err != nil {
			return fmt.Errorf("failed to call setup function: %w", err)
		}
	}

	p.plugins = append(p.plugins, plugin)

	return nil
}

func (p *PluginMiddleware) Close(ctx context.Context) {
	for _, plugin := range p.plugins {
		plugin.Close(ctx)
	}
	p.runtime.Close(ctx)
}

func (p *PluginMiddleware) call(ctx context.Context, name string, params ...uint64) error {
	p.exchange.Reset()

	for _, plugin := range p.plugins {
		f := plugin.ExportedFunction(name)
		if f == nil {
			continue
		}

		_, err := f.Call(ctx, params...)
		if err != nil {
			return fmt.Errorf("failed to call %s: %v", name, err)
		}
	}
	return nil
}
