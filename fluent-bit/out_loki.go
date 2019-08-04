package main

import "github.com/fluent/fluent-bit-go/output"
import "github.com/grafana/loki/pkg/promtail/client"
import "github.com/sirupsen/logrus"
import kit "github.com/go-kit/kit/log/logrus"
import "github.com/prometheus/common/model"
import "github.com/cortexproject/cortex/pkg/util/flagext"
import "github.com/json-iterator/go"

import (
	"C"
	"fmt"
	"os"
	"time"
	"unsafe"
)

var loki client.Client
var ls model.LabelSet
var plugin GoOutputPlugin = &fluentPlugin{}

type GoOutputPlugin interface {
	PluginConfigKey(ctx unsafe.Pointer, key string) string
	Unregister(ctx unsafe.Pointer)
	GetRecord(dec *output.FLBDecoder) (ret int, ts interface{}, rec map[interface{}]interface{})
	NewDecoder(data unsafe.Pointer, length int) *output.FLBDecoder
	HandleLine(ls model.LabelSet, timestamp time.Time, line string) error
	Exit(code int)
}

type fluentPlugin struct{}

func (p *fluentPlugin) PluginConfigKey(ctx unsafe.Pointer, key string) string {
	return output.FLBPluginConfigKey(ctx, key)
}

func (p *fluentPlugin) Unregister(ctx unsafe.Pointer) {
	output.FLBPluginUnregister(ctx)
}

func (p *fluentPlugin) GetRecord(dec *output.FLBDecoder) (int, interface{}, map[interface{}]interface{}) {
	return output.GetRecord(dec)
}

func (p *fluentPlugin) NewDecoder(data unsafe.Pointer, length int) *output.FLBDecoder {
	return output.NewDecoder(data, int(length))
}

func (p *fluentPlugin) Exit(code int) {
	os.Exit(code)
}

func (p *fluentPlugin) HandleLine(ls model.LabelSet, timestamp time.Time, line string) error {
	return loki.Handle(ls, timestamp, line)
}

//export FLBPluginRegister
func FLBPluginRegister(ctx unsafe.Pointer) int {
	return output.FLBPluginRegister(ctx, "loki", "Loki Output plugin written in GO!")
}

//export FLBPluginInit
// (fluentbit will call this)
// ctx (context) pointer to fluentbit context (state/ c code)
func FLBPluginInit(ctx unsafe.Pointer) int {
	// Example to retrieve an optional configuration parameter
	url := plugin.PluginConfigKey(ctx, "URL")
	batchWait := plugin.PluginConfigKey(ctx, "BatchWait")
	batchSize := plugin.PluginConfigKey(ctx, "BatchSize")
	labels := plugin.PluginConfigKey(ctx, "Labels")

	config, err := getLokiConfig(url, batchWait, batchSize, labels)
	if err != nil {
		plugin.Unregister(ctx)
		plugin.Exit(1)
		return output.FLB_ERROR
	}
	fmt.Printf("[flb-go] plugin URL parameter = '%s'\n", url)
	fmt.Printf("[flb-go] plugin BatchWait parameter = '%s'\n", batchSize)
	fmt.Printf("[flb-go] plugin BatchSize parameter = '%s'\n", batchWait)
	fmt.Printf("[flb-go] plugin Labels parameter = '%s'\n", labels)

	cfg := client.Config{}
	// Init everything with default values.
	flagext.RegisterFlags(&cfg)

	// Override some of those defaults
	cfg.URL = config.url
	cfg.BatchWait = config.batchWait
	cfg.BatchSize = config.batchSize

	log := logrus.New()

	loki, err = client.New(cfg, kit.NewLogrusLogger(log))
	if err != nil {
		log.Fatalf("client.New: %s\n", err)
		plugin.Unregister(ctx)
		plugin.Exit(1)
		return output.FLB_ERROR
	}
	ls = config.labelSet

	return output.FLB_OK
}

//export FLBPluginFlush
func FLBPluginFlush(data unsafe.Pointer, length C.int, tag *C.char) int {
	var ret int
	var ts interface{}
	var record map[interface{}]interface{}

	dec := plugin.NewDecoder(data, int(length))

	for {
		ret, ts, record = plugin.GetRecord(dec)
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
			fmt.Print("timestamp isn't known format. Use current time.")
			timestamp = time.Now()
		}

		line, err := createJSON(record)
		if err != nil {
			fmt.Printf("error creating message for Grafana Loki: %v", err)
			continue
		}

		err = plugin.HandleLine(ls, timestamp, line)
		if err != nil {
			fmt.Printf("error sending message for Grafana Loki: %v", err)
			return output.FLB_RETRY
		}
	}

	// Return options:
	//
	// output.FLB_OK    = data have been processed.
	// output.FLB_ERROR = unrecoverable error, do not try this again.
	// output.FLB_RETRY = retry to flush later.
	return output.FLB_OK
}

func createJSON(record map[interface{}]interface{}) (string, error) {
	m := make(map[string]interface{})

	for k, v := range record {
		switch t := v.(type) {
		case []byte:
			// prevent encoding to base64
			m[k.(string)] = string(t)
		default:
			m[k.(string)] = v
		}
	}

	js, err := jsoniter.Marshal(m)
	if err != nil {
		return "{}", err
	}

	return string(js), nil
}

//export FLBPluginExit
func FLBPluginExit() int {
	loki.Stop()
	return output.FLB_OK
}

func main() {
}
