package ingester

import (
	"context"
	"flag"
	"net/http"
	"sync"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/logproto"
)

// Config for an ingester.
type Config struct {
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`
}

// RegisterFlags registers the flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlags(f)
}

// Ingester builds chunks for incoming log streams.
type Ingester struct {
	cfg Config

	instancesMtx sync.RWMutex
	instances    map[string]*instance

	lifecycler *ring.Lifecycler
}

// New makes a new Ingester.
func New(cfg Config) (*Ingester, error) {
	i := &Ingester{
		cfg:       cfg,
		instances: map[string]*instance{},
	}

	var err error
	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i)
	if err != nil {
		return nil, err
	}

	return i, nil
}

// Shutdown stops the ingester.
func (i *Ingester) Shutdown() {
	i.lifecycler.Shutdown()
}

// StopIncomingRequests implements ring.Lifecycler.
func (i *Ingester) StopIncomingRequests() {

}

// Flush implements ring.Lifecycler.
func (i *Ingester) Flush() {

}

// TransferOut implements ring.Lifecycler.
func (i *Ingester) TransferOut(context.Context) error {
	return nil
}

// Push implements logproto.Pusher.
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
		inst = newInstance(instanceID)
		i.instances[instanceID] = inst
	}
	return inst
}

// Query the ingests for log streams matching a set of matchers.
func (i *Ingester) Query(req *logproto.QueryRequest, queryServer logproto.Querier_QueryServer) error {
	instanceID, err := user.ExtractOrgID(queryServer.Context())
	if err != nil {
		return err
	}

	instance := i.getOrCreateInstance(instanceID)
	return instance.Query(req, queryServer)
}

// Label returns the set of labels for the stream this ingester knows about.
func (i *Ingester) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	instanceID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	instance := i.getOrCreateInstance(instanceID)
	return instance.Label(ctx, req)
}

// Check implements grpc_health_v1.HealthCheck.
func (*Ingester) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

// Watch implements grpc_health_v1.HealthCheck.
func (*Ingester) Watch(*grpc_health_v1.HealthCheckRequest, grpc_health_v1.Health_WatchServer) error {
	return nil
}

// ReadinessHandler is used to indicate to k8s when the ingesters are ready for
// the addition removal of another ingester. Returns 204 when the ingester is
// ready, 500 otherwise.
func (i *Ingester) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	if i.lifecycler.IsReady(r.Context()) {
		w.WriteHeader(http.StatusNoContent)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
}
