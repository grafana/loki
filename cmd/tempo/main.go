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
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/tempo/pkg/distributor"
	"github.com/grafana/tempo/pkg/flagext"
	"github.com/grafana/tempo/pkg/ingester"
	"github.com/grafana/tempo/pkg/logproto"
	"github.com/grafana/tempo/pkg/querier"
)

type target struct {
	deps []string
	init func() error
	stop func()
}

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
		ingesterConfig    ingester.Config
		querierConfig     querier.Config
	)
	flagext.RegisterConfigs(flagset, &serverConfig, &ringConfig, &distributorConfig,
		&ingesterConfig, &querierConfig)
	flagset.Parse(os.Args[1:])
	util.InitLogger(&serverConfig)

	server, err := server.New(serverConfig)
	if err != nil {
		log.Fatalf("Error initializing server: %v", err)
	}
	defer server.Shutdown()

	var (
		r *ring.Ring
		d *distributor.Distributor
		i *ingester.Ingester
		q *querier.Querier

		mods = map[string]target{
			"ring": target{
				init: func() (err error) {
					r, err = ring.New(ringConfig)
					return
				},
				stop: func() {
					r.Stop()
				},
			},
			"distributor": target{
				deps: []string{"ring"},
				init: func() (err error) {
					d, err = distributor.New(distributorConfig, r)
					if err != nil {
						return
					}
					operationNameFunc := nethttp.OperationNameFunc(func(r *http.Request) string {
						return r.URL.RequestURI()
					})
					server.HTTP.Handle("/api/prom/push", middleware.Merge(
						middleware.Func(func(handler http.Handler) http.Handler {
							return nethttp.Middleware(opentracing.GlobalTracer(), handler, operationNameFunc)
						}),
						middleware.AuthenticateUser,
					).Wrap(http.HandlerFunc(d.PushHandler)))
					server.HTTP.Handle("/ring", r)
					return
				},
				stop: func() {},
			},
			"ingester": target{
				deps: []string{"ring"},
				init: func() (err error) {
					ingesterConfig.LifecyclerConfig.ListenPort = &serverConfig.GRPCListenPort
					i, err = ingester.New(ingesterConfig)
					if err != nil {
						return
					}
					logproto.RegisterPusherServer(server.GRPC, i)
					logproto.RegisterQuerierServer(server.GRPC, i)
					grpc_health_v1.RegisterHealthServer(server.GRPC, i)
					server.HTTP.Path("/ready").Handler(http.HandlerFunc(i.ReadinessHandler))
					return
				},
				stop: func() {
					i.Shutdown()
				},
			},
			"querier": target{
				deps: []string{"ring"},
				init: func() (err error) {
					q, err = querier.New(querierConfig, r)
					if err != nil {
						return
					}
					operationNameFunc := nethttp.OperationNameFunc(func(r *http.Request) string {
						return r.URL.RequestURI()
					})
					httpMiddleware := middleware.Merge(
						middleware.Func(func(handler http.Handler) http.Handler {
							return nethttp.Middleware(opentracing.GlobalTracer(), handler, operationNameFunc)
						}),
						middleware.AuthenticateUser,
					)
					server.HTTP.Handle("/api/prom/query", httpMiddleware.Wrap(http.HandlerFunc(q.QueryHandler)))
					server.HTTP.Handle("/api/prom/label", httpMiddleware.Wrap(http.HandlerFunc(q.LabelHandler)))
					server.HTTP.Handle("/api/prom/label/{name}/values", httpMiddleware.Wrap(http.HandlerFunc(q.LabelHandler)))
					return
				},
				stop: func() {},
			},
			"lite": target{
				deps: []string{"querier", "ingester", "distributor"},
				init: func() (err error) {
					return
				},
				stop: func() {},
			},
		}

		inited = map[string]struct{}{}
	)

	var run func(mod string)
	run = func(mod string) {
		if _, ok := inited[mod]; ok {
			return
		}
		for _, dep := range mods[mod].deps {
			run(dep)
		}
		if err := mods[mod].init(); err != nil {
			log.Fatalf("Error initializing %s: %v", mod, err)
		}
		inited[mod] = struct{}{}
	}

	run(flagset.Arg(0))

	server.Run()
}
