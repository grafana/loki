package main

import (
	"flag"

	log "github.com/sirupsen/logrus"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"github.com/weaveworks/cortex/pkg/ring"
	"github.com/weaveworks/cortex/pkg/util"
	"google.golang.org/grpc"

	"github.com/grafana/logish/pkg/distributor"
	"github.com/grafana/logish/pkg/logproto"
)

func main() {
	var (
		serverConfig = server.Config{
			MetricsNamespace: "logish",
			GRPCMiddleware: []grpc.UnaryServerInterceptor{
				middleware.ServerUserHeaderInterceptor,
			},
		}
		ringConfig        ring.Config
		distributorConfig distributor.Config
	)
	util.RegisterFlags(&distributorConfig)
	flag.Parse()

	r, err := ring.New(ringConfig)
	if err != nil {
		log.Fatalf("Error initializing ring: %v", err)
	}
	defer r.Stop()

	distributor, err := distributor.New(distributorConfig, r)
	if err != nil {
		log.Fatalf("Error initializing distributor: %v", err)
	}

	server, err := server.New(serverConfig)
	if err != nil {
		log.Fatalf("Error initializing server: %v", err)
	}
	defer server.Shutdown()

	logproto.RegisterAggregatorServer(server.GRPC, distributor)
	server.Run()
}
