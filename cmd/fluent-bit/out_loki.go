package main

import (
	"C"
	"fmt"
	"time"
	"unsafe"

	"github.com/fluent/fluent-bit-go/output"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"

	_ "github.com/grafana/loki/pkg/build"
)

var (
	// registered loki plugin instances, required for disposal during shutdown
	plugins []*loki
	logger  log.Logger
)

func init() {
	var logLevel logging.Level
	_ = logLevel.Set("info")
	logger = newLogger(logLevel)
}

type pluginConfig struct {
	ctx unsafe.Pointer
}

func (c *pluginConfig) Get(key string) string {
	return output.FLBPluginConfigKey(c.ctx, key)
}

//export FLBPluginRegister
func FLBPluginRegister(ctx unsafe.Pointer) int {
	return output.FLBPluginRegister(ctx, "loki", "Ship fluent-bit logs to Grafana Loki")
}

//export FLBPluginInit
// (fluentbit will call this)
// ctx (context) pointer to fluentbit context (state/ c code)
func FLBPluginInit(ctx unsafe.Pointer) int {
	conf, err := parseConfig(&pluginConfig{ctx: ctx})
	if err != nil {
		level.Error(logger).Log("[flb-go]", "failed to launch", "error", err)
		return output.FLB_ERROR
	}

	// numeric plugin ID, only used for user-facing purpose (logging, ...)
	id := len(plugins)
	logger := log.With(newLogger(conf.logLevel), "id", id)

	level.Info(logger).Log("[flb-go]", "Starting fluent-bit-go-loki", "version", version.Info())
	paramLogger := log.With(logger, "[flb-go]", "provided parameter")
	level.Info(paramLogger).Log("URL", conf.clientConfig.URL)
	level.Info(paramLogger).Log("TenantID", conf.clientConfig.TenantID)
	level.Info(paramLogger).Log("BatchWait", fmt.Sprintf("%.3fs", conf.clientConfig.BatchWait.Seconds()))
	level.Info(paramLogger).Log("BatchSize", conf.clientConfig.BatchSize)
	level.Info(paramLogger).Log("Timeout", fmt.Sprintf("%.3fs", conf.clientConfig.Timeout.Seconds()))
	level.Info(paramLogger).Log("MinBackoff", fmt.Sprintf("%.3fs", conf.clientConfig.BackoffConfig.MinBackoff.Seconds()))
	level.Info(paramLogger).Log("MaxBackoff", fmt.Sprintf("%.3fs", conf.clientConfig.BackoffConfig.MaxBackoff.Seconds()))
	level.Info(paramLogger).Log("MaxRetries", conf.clientConfig.BackoffConfig.MaxRetries)
	level.Info(paramLogger).Log("Labels", conf.clientConfig.ExternalLabels)
	level.Info(paramLogger).Log("LogLevel", conf.logLevel.String())
	level.Info(paramLogger).Log("AutoKubernetesLabels", conf.autoKubernetesLabels)
	level.Info(paramLogger).Log("RemoveKeys", fmt.Sprintf("%+v", conf.removeKeys))
	level.Info(paramLogger).Log("LabelKeys", fmt.Sprintf("%+v", conf.labelKeys))
	level.Info(paramLogger).Log("LineFormat", conf.lineFormat)
	level.Info(paramLogger).Log("DropSingleKey", conf.dropSingleKey)
	level.Info(paramLogger).Log("LabelMapPath", fmt.Sprintf("%+v", conf.labelMap))
	level.Info(paramLogger).Log("Buffer", conf.bufferConfig.buffer)
	level.Info(paramLogger).Log("BufferType", conf.bufferConfig.bufferType)
	level.Info(paramLogger).Log("DqueDir", conf.bufferConfig.dqueConfig.queueDir)
	level.Info(paramLogger).Log("DqueSegmentSize", conf.bufferConfig.dqueConfig.queueSegmentSize)
	level.Info(paramLogger).Log("DqueSync", conf.bufferConfig.dqueConfig.queueSync)
	plugin, err := newPlugin(conf, logger)
	if err != nil {
		level.Error(logger).Log("newPlugin", err)
		return output.FLB_ERROR
	}

	// register plugin instance, to be retrievable when sending logs
	output.FLBPluginSetContext(ctx, plugin)
	// remember plugin instance, required to cleanly dispose when fluent-bit is shutting down
	plugins = append(plugins, plugin)

	return output.FLB_OK
}

//export FLBPluginFlushCtx
func FLBPluginFlushCtx(ctx, data unsafe.Pointer, length C.int, _ *C.char) int {
	plugin := output.FLBPluginGetContext(ctx).(*loki)
	if plugin == nil {
		level.Error(logger).Log("[flb-go]", "plugin not initialized")
		return output.FLB_ERROR
	}

	var ret int
	var ts interface{}
	var record map[interface{}]interface{}

	dec := output.NewDecoder(data, int(length))

	for {
		ret, ts, record = output.GetRecord(dec)
		if ret != 0 {
			break
		}

		// Get timestamp
		var timestamp time.Time
		switch t := ts.(type) {
		case output.FLBTime:
			timestamp = ts.(output.FLBTime).Time
		case uint64:
			timestamp = time.Unix(int64(t), 0)
		default:
			level.Warn(plugin.logger).Log("msg", "timestamp isn't known format. Use current time.")
			timestamp = time.Now()
		}

		err := plugin.sendRecord(record, timestamp)
		if err != nil {
			level.Error(plugin.logger).Log("msg", "error sending record to Loki", "error", err)
			return output.FLB_ERROR
		}
	}

	// Return options:
	//
	// output.FLB_OK    = data have been processed.
	// output.FLB_ERROR = unrecoverable error, do not try this again.
	// output.FLB_RETRY = retry to flush later.
	return output.FLB_OK
}

//export FLBPluginExit
func FLBPluginExit() int {
	for _, plugin := range plugins {
		if plugin.client != nil {
			plugin.client.Stop()
		}
	}
	return output.FLB_OK
}

func main() {}
