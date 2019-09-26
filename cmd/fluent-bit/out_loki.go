package main

import (
	"C"
	"time"
	"unsafe"

	"github.com/fluent/fluent-bit-go/output"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"
)
import "fmt"

var plugin *loki
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

//export FLBPluginInit
// (fluentbit will call this)
// ctx (context) pointer to fluentbit context (state/ c code)
func FLBPluginInit(ctx unsafe.Pointer) int {

	conf, err := parseConfig(&pluginConfig{ctx: ctx})
	if err != nil {
		level.Error(logger).Log("[flb-go]", "failed to launch", "error", err)
		return output.FLB_ERROR
	}
	logger = newLogger(conf.logLevel)
	level.Info(logger).Log("[flb-go]", "Starting fluent-bit-go-loki", "version", version.Info())
	level.Info(logger).Log("[flb-go]", "provided parameter", "URL", conf.clientConfig.URL)
	level.Info(logger).Log("[flb-go]", "provided parameter", "BatchWait", conf.clientConfig.BatchWait)
	level.Info(logger).Log("[flb-go]", "provided parameter", "BatchSize", conf.clientConfig.BatchSize)
	level.Info(logger).Log("[flb-go]", "provided parameter", "Labels", conf.clientConfig.ExternalLabels)
	level.Info(logger).Log("[flb-go]", "provided parameter", "LogLevel", conf.logLevel)
	level.Info(logger).Log("[flb-go]", "provided parameter", "RemoveKeys", fmt.Sprintf("%+v", conf.removeKeys))
	level.Info(logger).Log("[flb-go]", "provided parameter", "LabelKeys", fmt.Sprintf("%+v", conf.labelKeys))
	level.Info(logger).Log("[flb-go]", "provided parameter", "LineFormat", conf.lineFormat)
	level.Info(logger).Log("[flb-go]", "provided parameter", "DropSingleKey", conf.dropSingleKey)
	level.Info(logger).Log("[flb-go]", "provided parameter", "LabelMapPath", fmt.Sprintf("%+v", conf.labeMap))

	plugin, err = newPlugin(conf, logger)
	if err != nil {
		level.Error(logger).Log("newPlugin", err)
		return output.FLB_ERROR
	}

	return output.FLB_OK
}

//export FLBPluginFlush
func FLBPluginFlush(data unsafe.Pointer, length C.int, tag *C.char) int {
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
			level.Warn(logger).Log("msg", "timestamp isn't known format. Use current time.")
			timestamp = time.Now()
		}

		err := plugin.sendRecord(record, timestamp)
		if err != nil {
			level.Error(logger).Log("msg", "error sending record to Loki", "error", err)
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
	if plugin.client != nil {
		plugin.client.Stop()
	}
	return output.FLB_OK
}

func main() {}
