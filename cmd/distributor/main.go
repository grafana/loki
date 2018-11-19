package main

import (
	"flag"
	"net/http"
	"os"

	"github.com/opentracing-contrib/go-stdlib/nethttp"
	opentracing "github.com/opentracing/opentracing-go"
	log "github.com/sirupsen/logrus"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/server"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"
	"google.golang.org/grpc"

	"github.com/grafana/tempo/pkg/distributor"
	"github.com/grafana/tempo/pkg/flagext"
)

func main() {
	var (
		flagset      = flag.NewFlagSet("", flag.ExitOnError)
		serverConfig = server.Config{
			MetricsNamespace: "tempo",
			GRPCMiddleware: []grpc.UnaryServerInterceptor{
				middleware.ServerUserHeaderInterceptor,
			},
		}
		ringConfig        ring.Config
		distributorConfig distributor.Config
	)
	flagext.RegisterConfigs(flagset, &serverConfig, &ringConfig, &distributorConfig)
	flagset.Parse(os.Args[1:])
	util.InitLogger(&serverConfig)

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

	operationNameFunc := nethttp.OperationNameFunc(func(r *http.Request) string {
		return r.URL.RequestURI()
	})
	server.HTTP.Handle("/api/prom/push", middleware.Merge(
		middleware.Func(func(handler http.Handler) http.Handler {
			return nethttp.Middleware(opentracing.GlobalTracer(), handler, operationNameFunc)
		}),
		middleware.AuthenticateUser,
	).Wrap(http.HandlerFunc(distributor.PushHandler)))
	server.HTTP.Handle("/ring", r)
	server.Run()
}
