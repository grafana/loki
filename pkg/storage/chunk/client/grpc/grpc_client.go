package grpc

import (
	"flag"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Config for a StorageClient
type Config struct {
	Address string `yaml:"server_address,omitempty"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Address, "grpc-store.server-address", "", "Hostname or IP of the gRPC store instance.")
}

func connectToGrpcServer(serverAddress string) (GrpcStoreClient, *grpc.ClientConn, error) {
	params := keepalive.ClientParameters{
		Time:                time.Second * 20,
		Timeout:             time.Second * 10,
		PermitWithoutStream: true,
	}
	param := grpc.WithKeepaliveParams(params)

	// nolint:staticcheck // grpc.Dial() has been deprecated; we'll address it before upgrading to gRPC 2.
	cc, err := grpc.Dial(serverAddress, param, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to dial grpc-store %s", serverAddress)
	}
	return NewGrpcStoreClient(cc), cc, nil
}
