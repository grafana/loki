package main

import (
	"flag"
	"net/http"
	"os"

	"github.com/prometheus/common/promlog"
	log "github.com/sirupsen/logrus"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"github.com/weaveworks/cortex/pkg/util"
	"google.golang.org/grpc"

	"github.com/grafana/tempo/pkg/flagext"
	"github.com/grafana/tempo/pkg/ingester"
	"github.com/grafana/tempo/pkg/logproto"
)

func main() {
	var (
		flagset      = flag.NewFlagSet("", flag.ExitOnError)
		serverConfig = server.Config{
			MetricsNamespace: "tempo",
			GRPCMiddleware: []grpc.UnaryServerInterceptor{
				middleware.ServerUserHeaderInterceptor,
			},
			GRPCStreamMiddleware: []grpc.StreamServerInterceptor{
				middleware.StreamServerUserHeaderInterceptor,
			},
		}
		ingesterConfig ingester.Config
		logLevel       = promlog.AllowedLevel{}
	)
	flagext.Var(flagset, &logLevel, "log.level", "info", "")
	flagext.RegisterConfigs(flagset, &serverConfig, &ingesterConfig)
	flagset.Parse(os.Args[1:])

	logging.Setup(logLevel.String())
	util.InitLogger(logLevel)

	ingesterConfig.LifecyclerConfig.ListenPort = &serverConfig.GRPCListenPort
	ingester, err := ingester.New(ingesterConfig)
	if err != nil {
		log.Fatalf("Error initializing ingester: %v", err)
	}
	defer ingester.Shutdown()

	server, err := server.New(serverConfig)
	if err != nil {
		log.Fatalf("Error initializing server: %v", err)
	}
	defer server.Shutdown()

	logproto.RegisterPusherServer(server.GRPC, ingester)
	logproto.RegisterQuerierServer(server.GRPC, ingester)
	server.HTTP.Path("/ready").Handler(http.HandlerFunc(ingester.ReadinessHandler))
	server.Run()
}
