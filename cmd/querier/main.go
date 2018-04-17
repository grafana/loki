package main

import (
	"flag"
	"net/http"

	log "github.com/sirupsen/logrus"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"
	"github.com/weaveworks/cortex/pkg/ring"
	"github.com/weaveworks/cortex/pkg/util"
	"google.golang.org/grpc"

	"github.com/grafana/logish/pkg/querier"
)

func main() {
	var (
		serverConfig = server.Config{
			MetricsNamespace: "logish",
			GRPCMiddleware: []grpc.UnaryServerInterceptor{
				middleware.ServerUserHeaderInterceptor,
			},
		}
		ringConfig    ring.Config
		querierConfig querier.Config
	)
	util.RegisterFlags(&serverConfig, &ringConfig, &querierConfig)
	flag.Parse()

	r, err := ring.New(ringConfig)
	if err != nil {
		log.Fatalf("Error initializing ring: %v", err)
	}
	defer r.Stop()

	querier, err := querier.New(querierConfig, r)
	if err != nil {
		log.Fatalf("Error initializing querier: %v", err)
	}

	server, err := server.New(serverConfig)
	if err != nil {
		log.Fatalf("Error initializing server: %v", err)
	}
	defer server.Shutdown()

	server.HTTP.Handle("/api/query", http.HandlerFunc(querier.QueryHandler))
	server.Run()
}
