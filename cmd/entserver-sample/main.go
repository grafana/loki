package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/entitlement"
	grpc "google.golang.org/grpc"
	"gopkg.in/yaml.v2"
)

type entitlementOptions struct {
	grpcPort   int
	logLevel   string
	configFile string
}

type configRoot struct {
	Entitlements []configItem `yaml:"entitlements"`
}

type configItem struct {
	OrgID   string   `yaml:"orgid"`
	Readers []string `yaml:"readers"`
	Writers []string `yaml:"writers"`
}

var options entitlementOptions = entitlementOptions{
	grpcPort:   21001,
	configFile: "entserver-sample.yml",
}

type entitlementService struct {
}

var logger log.Logger
var config configRoot

type configMapItem struct {
	readers map[string]bool
	writers map[string]bool
}

var configMap map[string]configMapItem = make(map[string]configMapItem)

func (*entitlementService) Entitled(ctx context.Context, req *entitlement.EntitlementRequest) (*entitlement.EntitlementResponse, error) {
	var entitled bool
	c := configMap[req.OrgID]
	switch strings.ToLower(req.Action) {
	case "write":
		if ok, value := c.writers[req.UserID]; ok {
			entitled = value
		} else if ok, value := c.writers["*"]; ok {
			entitled = value
		}
	default:
		if ok, value := c.readers[req.UserID]; ok {
			entitled = value
		} else if ok, value := c.readers["*"]; ok {
			entitled = value
		}
	}
	level.Debug(logger).Log("msg", fmt.Sprintf("OrgID:%s, UserID:%s, Action:%s, LabelValue:%s->%v", req.OrgID, req.UserID, req.Action, req.LabelValue, entitled))

	res := &entitlement.EntitlementResponse{Entitled: entitled}
	return res, nil
}

func parseFlags() {
	flag.IntVar(&options.grpcPort, "grpcPort", options.grpcPort, "gRPC port")
	flag.StringVar(&options.configFile, "configFile", options.configFile, "config file path")
	flag.StringVar(&options.logLevel, "logLevel", options.logLevel, "NONE, DEBUG, INFO, WARNING, ERROR")
	flag.Parse()
}

func main() {
	parseFlags()
	w := log.NewSyncWriter(os.Stderr)
	logger = log.NewLogfmtLogger(w)
	logger = log.With(logger, "ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller)
	switch strings.ToUpper(options.logLevel) {
	case "ERROR":
		logger = level.NewFilter(logger, level.AllowError())
	case "WARNING":
		logger = level.NewFilter(logger, level.AllowWarn())
	case "INFO":
		logger = level.NewFilter(logger, level.AllowInfo())
	case "DEBUG":
		logger = level.NewFilter(logger, level.AllowDebug())
	case "NONE":
		logger = level.NewFilter(logger, level.AllowNone())
	default:
		logger = level.NewFilter(logger, level.AllowInfo())
		//
	}
	level.Info(logger).Log("msg", "entserver started")

	listenPort, err := net.Listen("tcp", fmt.Sprintf(":%d", options.grpcPort))
	if err != nil {
		panic(err)
	}

	data, err := ioutil.ReadFile(options.configFile)
	if err != nil {
		panic(err)
	}

	err = yaml.UnmarshalStrict(data, &config)
	if err != nil {
		panic(err)
	}

	level.Info(logger).Log("config", fmt.Sprintf("%+v", config))

	for _, configItem := range config.Entitlements {
		// convert readers/writers to map for faster query
		configMap[configItem.OrgID] = configMapItem{
			readers: make(map[string]bool),
			writers: make(map[string]bool),
		}
		c := configMap[configItem.OrgID]
		for _, useritem := range configItem.Writers {
			c.writers[useritem] = true
		}
		for _, user := range configItem.Readers {
			c.readers[user] = true
		}
	}

	level.Info(logger).Log("configMap", fmt.Sprintf("%+v", configMap))

	server := grpc.NewServer()
	service := &entitlementService{}
	entitlement.RegisterEntitlementServer(server, service)
	server.Serve(listenPort)
}
