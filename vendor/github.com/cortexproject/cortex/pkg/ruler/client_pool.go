package ruler

import (
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// ClientsPool is the interface used to get the client from the pool for a specified address.
type ClientsPool interface {
	services.Service
	// GetClientFor returns the ruler client for the given address.
	GetClientFor(addr string) (RulerClient, error)
}

type rulerClientsPool struct {
	*client.Pool
}

func (p *rulerClientsPool) GetClientFor(addr string) (RulerClient, error) {
	c, err := p.Pool.GetClientFor(addr)
	if err != nil {
		return nil, err
	}
	return c.(RulerClient), nil
}

func newRulerClientPool(clientCfg grpcclient.Config, logger log.Logger, reg prometheus.Registerer) ClientsPool {
	// We prefer sane defaults instead of exposing further config options.
	poolCfg := client.PoolConfig{
		CheckInterval:      time.Minute,
		HealthCheckEnabled: true,
		HealthCheckTimeout: 10 * time.Second,
	}

	clientsCount := promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Name: "cortex_ruler_clients",
		Help: "The current number of ruler clients in the pool.",
	})

	return &rulerClientsPool{
		client.NewPool("ruler", poolCfg, nil, newRulerClientFactory(clientCfg, reg), clientsCount, logger),
	}
}

func newRulerClientFactory(clientCfg grpcclient.Config, reg prometheus.Registerer) client.PoolFactory {
	requestDuration := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cortex_ruler_client_request_duration_seconds",
		Help:    "Time spent executing requests to the ruler.",
		Buckets: prometheus.ExponentialBuckets(0.008, 4, 7),
	}, []string{"operation", "status_code"})

	return func(addr string) (client.PoolClient, error) {
		return dialRulerClient(clientCfg, addr, requestDuration)
	}
}

func dialRulerClient(clientCfg grpcclient.Config, addr string, requestDuration *prometheus.HistogramVec) (*rulerExtendedClient, error) {
	opts, err := clientCfg.DialOption(grpcclient.Instrument(requestDuration))
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to dial ruler %s", addr)
	}

	return &rulerExtendedClient{
		RulerClient:  NewRulerClient(conn),
		HealthClient: grpc_health_v1.NewHealthClient(conn),
		conn:         conn,
	}, nil
}

type rulerExtendedClient struct {
	RulerClient
	grpc_health_v1.HealthClient
	conn *grpc.ClientConn
}

func (c *rulerExtendedClient) Close() error {
	return c.conn.Close()
}

func (c *rulerExtendedClient) String() string {
	return c.RemoteAddress()
}

func (c *rulerExtendedClient) RemoteAddress() string {
	return c.conn.Target()
}
