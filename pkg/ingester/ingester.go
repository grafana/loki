package ingester

import (
	"context"
	"flag"
	"sync"

	"github.com/weaveworks/common/user"
	"github.com/weaveworks/cortex/pkg/ring"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/logish/pkg/logproto"
)

type Config struct {
	LifecyclerConfig ring.LifecyclerConfig
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlags(f)
}

type Ingester struct {
	cfg Config
	r   ring.ReadRing

	instancesMtx sync.RWMutex
	instances    map[string]*instance

	lifecycler *ring.Lifecycler
}

func New(cfg Config, r ring.ReadRing) (*Ingester, error) {
	i := &Ingester{
		cfg:       cfg,
		r:         r,
		instances: map[string]*instance{},
	}

	var err error
	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i)
	if err != nil {
		return nil, err
	}

	return i, nil
}

func (i *Ingester) Shutdown() {
	i.lifecycler.Shutdown()
}

func (i *Ingester) StopIncomingRequests() {

}

func (i *Ingester) Flush() {

}

func (i *Ingester) Transfer() error {
	return nil
}

func (i *Ingester) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	instanceID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	instance := i.getOrCreateInstance(instanceID)
	err = instance.Push(ctx, req)
	return &logproto.PushResponse{}, err
}

func (i *Ingester) getOrCreateInstance(instanceID string) *instance {
	i.instancesMtx.RLock()
	inst, ok := i.instances[instanceID]
	i.instancesMtx.RUnlock()
	if ok {
		return inst
	}

	i.instancesMtx.Lock()
	defer i.instancesMtx.Unlock()
	inst, ok = i.instances[instanceID]
	if !ok {
		inst = &instance{}
		i.instances[instanceID] = inst
	}
	return inst
}

func (i *Ingester) Query(req *logproto.QueryRequest, queryServer logproto.Querier_QueryServer) error {
	instanceID, err := user.ExtractOrgID(queryServer.Context())
	if err != nil {
		return err
	}

	instance := i.getOrCreateInstance(instanceID)
	return instance.Query(req, queryServer)
}

func (*Ingester) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}
