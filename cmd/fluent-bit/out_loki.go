package main

import (
	"C"
	"fmt"
	"time"
	"unsafe"

	"github.com/fluent/fluent-bit-go/output"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	_ "github.com/grafana/loki/pkg/build"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"
)

var plugins []*loki
var logger log.Logger

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

func newLokiOutput(ctx unsafe.Pointer, lokiID int) (*loki, error) {
	conf, err := parseConfig(&pluginConfig{ctx: ctx})
	if err != nil {
		level.Error(logger).Log("[flb-go]", "failed to launch", "error", err)
		return nil, err
	}
	logger = newLogger(conf.logLevel)
	level.Info(logger).Log("[flb-go]", "Starting fluent-bit-go-loki", "version", version.Info())
	level.Info(logger).Log("[flb-go]", "instance ID", lokiID)
	level.Info(logger).Log("[flb-go]", "provided parameter", "URL", conf.clientConfig.URL)
	level.Info(logger).Log("[flb-go]", "provided parameter", "TenantID", conf.clientConfig.TenantID)
	level.Info(logger).Log("[flb-go]", "provided parameter", "BatchWait", conf.clientConfig.BatchWait)
	level.Info(logger).Log("[flb-go]", "provided parameter", "BatchSize", conf.clientConfig.BatchSize)
	level.Info(logger).Log("[flb-go]", "provided parameter", "Labels", conf.clientConfig.ExternalLabels)
	level.Info(logger).Log("[flb-go]", "provided parameter", "LogLevel", conf.logLevel)
	level.Info(logger).Log("[flb-go]", "provided parameter", "AutoKubernetesLabels", conf.autoKubernetesLabels)
	level.Info(logger).Log("[flb-go]", "provided parameter", "RemoveKeys", fmt.Sprintf("%+v", conf.removeKeys))
	level.Info(logger).Log("[flb-go]", "provided parameter", "LabelKeys", fmt.Sprintf("%+v", conf.labelKeys))
	level.Info(logger).Log("[flb-go]", "provided parameter", "LineFormat", conf.lineFormat)
	level.Info(logger).Log("[flb-go]", "provided parameter", "DropSingleKey", conf.dropSingleKey)
	level.Info(logger).Log("[flb-go]", "provided parameter", "LabelMapPath", fmt.Sprintf("%+v", conf.labelMap))

	plugin, err := newPlugin(conf, logger)
	return plugin, err
}

func addLokiOutput(ctx unsafe.Pointer) error {
	lokiID := len(plugins)
	// Set the context to point to any Go variable
	output.FLBPluginSetContext(ctx, lokiID)
	plugin, err := newLokiOutput(ctx, lokiID)
	if err != nil {
		return err
	}

	plugins = append(plugins, plugin)
	return nil
}

func getLokiOutput(ctx unsafe.Pointer) (*loki, int) {
	lokiID := output.FLBPluginGetContext(ctx).(int)
	return plugins[lokiID], lokiID
}

//export FLBPluginInit
// (fluentbit will call this)
// ctx (context) pointer to fluentbit context (state/ c code)
func FLBPluginInit(ctx unsafe.Pointer) int {

	err := addLokiOutput(ctx)
	if err != nil {
		level.Error(logger).Log("newPlugin", err)
		return output.FLB_ERROR
	}

	return output.FLB_OK
}

//export FLBPluginFlushCtx
func FLBPluginFlushCtx(ctx, data unsafe.Pointer, length C.int, tag *C.char) int {
	plugin, lokiID := getLokiOutput(ctx)
	if plugin == nil {
		level.Error(logger).Log("[flb-go]", "plugin not initialized", "lokiID", lokiID)
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
