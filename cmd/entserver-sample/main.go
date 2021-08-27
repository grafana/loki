/*
(c) 2021 Morgan Stanley

THIS SOFTWARE IS CONTRIBUTED SUBJECT TO THE TERMS OF THE Grafana Labs Software
Grant and Contributor License Agreement, V 2021-04-20 GNU.

THIS SOFTWARE IS LICENSED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE AND ANY
WARRANTY OF NON-INFRINGEMENT, ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT
OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR
BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER
IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
POSSIBILITY OF SUCH DAMAGE. THIS SOFTWARE MAY BE REDISTRIBUTED TO OTHERS ONLY
BY EFFECTIVELY USING THIS OR ANOTHER EQUIVALENT DISCLAIMER IN ADDITION TO ANY
OTHER REQUIRED LICENSE TERMS.
*/

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
	grpc "google.golang.org/grpc"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/entitlement"
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
	err = server.Serve(listenPort)
	if err != nil {
		panic(err)
	}
}
