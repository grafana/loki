package main

import (
	"flag"
	"net/http"
	"os"

	"github.com/cortexproject/cortex/pkg/util"
	log "github.com/sirupsen/logrus"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

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
	)
	flagext.RegisterConfigs(flagset, &serverConfig, &ingesterConfig)
	flagset.Parse(os.Args[1:])

	util.InitLogger(&serverConfig)

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
	grpc_health_v1.RegisterHealthServer(server.GRPC, ingester)
	server.HTTP.Path("/ready").Handler(http.HandlerFunc(ingester.ReadinessHandler))
	server.Run()
}
