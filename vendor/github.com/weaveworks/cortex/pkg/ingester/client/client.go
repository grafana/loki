package client

import (
	"flag"

	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/mwitkow/go-grpc-middleware"
	"github.com/opentracing/opentracing-go"
	"github.com/weaveworks/common/middleware"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip" // get gzip compressor registered
)

type closableIngesterClient struct {
	IngesterClient
	conn *grpc.ClientConn
	cfg  Config
}

// MakeIngesterClient makes a new IngesterClient
func MakeIngesterClient(addr string, cfg Config) (IngesterClient, error) {
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(grpc_middleware.ChainUnaryClient(
			otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
			middleware.ClientUserHeaderInterceptor,
		)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(cfg.MaxRecvMsgSize)),
	}
	if cfg.legacyCompressToIngester {
		cfg.CompressToIngester = true
	}
	if cfg.CompressToIngester {
		opts = append(opts, grpc.WithDefaultCallOptions(grpc.UseCompressor("gzip")))
	}
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	return &closableIngesterClient{
		IngesterClient: NewIngesterClient(conn),
		conn:           conn,
	}, nil
}

func (c *closableIngesterClient) Close() error {
	return c.conn.Close()
}

// Config is the configuration struct for the ingester client
type Config struct {
	MaxRecvMsgSize           int
	CompressToIngester       bool
	legacyCompressToIngester bool
}

// RegisterFlags registers configuration settings used by the ingester client config
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	// lite uses both ingester.Config and distributor.Config.
	// Both of them has IngesterClientConfig, so calling RegisterFlags on them triggers panic.
	// This check ignores second call to RegisterFlags on IngesterClientConfig and then populates it manually with SetClientConfig
	if flag.Lookup("ingester.client.max-recv-message-size") != nil {
		return
	}
	// We have seen 20MB returns from queries - add a bit of headroom
	f.IntVar(&cfg.MaxRecvMsgSize, "ingester.client.max-recv-message-size", 64*1024*1024, "Maximum message size, in bytes, this client will receive.")
	flag.BoolVar(&cfg.CompressToIngester, "ingester.client.compress-to-ingester", false, "Compress data in calls to ingesters.")
	// moved from distributor pkg, but flag prefix left as back compat fallback for existing users.
	flag.BoolVar(&cfg.legacyCompressToIngester, "distributor.compress-to-ingester", false, "Compress data in calls to ingesters. (DEPRECATED: use ingester.client.compress-to-ingester instead")
}
