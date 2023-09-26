package kuberesolver

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

const (
	kubernetesSchema = "kubernetes"
	defaultFreq      = time.Minute * 30
)

var (
	endpointsForTarget = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kuberesolver_endpoints_total",
			Help: "The number of endpoints for a given target",
		},
		[]string{"target"},
	)
	addressesForTarget = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kuberesolver_addresses_total",
			Help: "The number of addresses for a given target",
		},
		[]string{"target"},
	)
)

type targetInfo struct {
	serviceName       string
	serviceNamespace  string
	port              string
	resolveByPortName bool
	useFirstPort      bool
}

func (ti targetInfo) String() string {
	return fmt.Sprintf("kubernetes://%s/%s:%s", ti.serviceNamespace, ti.serviceName, ti.port)
}

// RegisterInCluster registers the kuberesolver builder to grpc with kubernetes schema
func RegisterInCluster() {
	RegisterInClusterWithSchema(kubernetesSchema)
}

// RegisterInClusterWithSchema registers the kuberesolver builder to the grpc with custom schema
func RegisterInClusterWithSchema(schema string) {
	resolver.Register(NewBuilder(nil, schema))
}

// NewBuilder creates a kubeBuilder which is used by grpc resolver.
func NewBuilder(client K8sClient, schema string) resolver.Builder {
	return &kubeBuilder{
		k8sClient: client,
		schema:    schema,
	}
}

type kubeBuilder struct {
	k8sClient K8sClient
	schema    string
}

func splitServicePortNamespace(hpn string) (service, port, namespace string) {
	service = hpn

	colon := strings.LastIndexByte(service, ':')
	if colon != -1 {
		service, port = service[:colon], service[colon+1:]
	}

	dot := strings.LastIndexByte(service, '.')
	if dot != -1 {
		service, namespace = service[:dot], service[dot+1:]
	}

	return
}

func parseResolverTarget(target resolver.Target) (targetInfo, error) {
	var service, port, namespace string
	if target.URL.Host == "" {
		// kubernetes:///service.namespace:port
		service, port, namespace = splitServicePortNamespace(target.Endpoint())
	} else if target.URL.Port() == "" && target.Endpoint() != "" {
		// kubernetes://namespace/service:port
		service, port, _ = splitServicePortNamespace(target.Endpoint())
		namespace = target.URL.Hostname()
	} else {
		// kubernetes://service.namespace:port
		service, port, namespace = splitServicePortNamespace(target.URL.Host)
	}

	if service == "" {
		return targetInfo{}, fmt.Errorf("target %s must specify a service", &target.URL)
	}

	resolveByPortName := false
	useFirstPort := false
	if port == "" {
		useFirstPort = true
	} else if _, err := strconv.Atoi(port); err != nil {
		resolveByPortName = true
	}

	return targetInfo{
		serviceName:       service,
		serviceNamespace:  namespace,
		port:              port,
		resolveByPortName: resolveByPortName,
		useFirstPort:      useFirstPort,
	}, nil
}

// Build creates a new resolver for the given target.
//
// gRPC dial calls Build synchronously, and fails if the returned error is
// not nil.
func (b *kubeBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	if b.k8sClient == nil {
		if cl, err := NewInClusterK8sClient(); err == nil {
			b.k8sClient = cl
		} else {
			return nil, err
		}
	}
	ti, err := parseResolverTarget(target)
	if err != nil {
		return nil, err
	}
	if ti.serviceNamespace == "" {
		ti.serviceNamespace = getCurrentNamespaceOrDefault()
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &kResolver{
		target:    ti,
		ctx:       ctx,
		cancel:    cancel,
		cc:        cc,
		rn:        make(chan struct{}, 1),
		k8sClient: b.k8sClient,
		t:         time.NewTimer(defaultFreq),
		freq:      defaultFreq,

		endpoints: endpointsForTarget.WithLabelValues(ti.String()),
		addresses: addressesForTarget.WithLabelValues(ti.String()),
	}
	go until(func() {
		r.wg.Add(1)
		err := r.watch()
		if err != nil && err != io.EOF {
			grpclog.Errorf("kuberesolver: watching ended with error='%v', will reconnect again", err)
		}
	}, time.Second, ctx.Done())
	return r, nil
}

// Scheme returns the scheme supported by this resolver.
// Scheme is defined at https://github.com/grpc/grpc/blob/master/doc/naming.md.
func (b *kubeBuilder) Scheme() string {
	return b.schema
}

type kResolver struct {
	target targetInfo
	ctx    context.Context
	cancel context.CancelFunc
	cc     resolver.ClientConn
	// rn channel is used by ResolveNow() to force an immediate resolution of the target.
	rn        chan struct{}
	k8sClient K8sClient
	// wg is used to enforce Close() to return after the watcher() goroutine has finished.
	wg   sync.WaitGroup
	t    *time.Timer
	freq time.Duration

	endpoints prometheus.Gauge
	addresses prometheus.Gauge
}

// ResolveNow will be called by gRPC to try to resolve the target name again.
// It's just a hint, resolver can ignore this if it's not necessary.
func (k *kResolver) ResolveNow(resolver.ResolveNowOptions) {
	select {
	case k.rn <- struct{}{}:
	default:
	}
}

// Close closes the resolver.
func (k *kResolver) Close() {
	k.cancel()
	k.wg.Wait()
}

func (k *kResolver) makeAddresses(e Endpoints) ([]resolver.Address, string) {
	var newAddrs []resolver.Address
	for _, subset := range e.Subsets {
		port := ""
		if k.target.useFirstPort {
			port = strconv.Itoa(subset.Ports[0].Port)
		} else if k.target.resolveByPortName {
			for _, p := range subset.Ports {
				if p.Name == k.target.port {
					port = strconv.Itoa(p.Port)
					break
				}
			}
		} else {
			port = k.target.port
		}

		if len(port) == 0 {
			port = strconv.Itoa(subset.Ports[0].Port)
		}

		for _, address := range subset.Addresses {
			newAddrs = append(newAddrs, resolver.Address{
				Type:       resolver.Backend,
				Addr:       net.JoinHostPort(address.IP, port),
				ServerName: fmt.Sprintf("%s.%s", k.target.serviceName, k.target.serviceNamespace),
				Metadata:   nil,
			})
		}
	}
	return newAddrs, ""
}

func (k *kResolver) handle(e Endpoints) {
	result, _ := k.makeAddresses(e)
	//	k.cc.NewServiceConfig(sc)
	if len(result) > 0 {
		k.cc.NewAddress(result)
	}

	k.endpoints.Set(float64(len(e.Subsets)))
	k.addresses.Set(float64(len(result)))
}

func (k *kResolver) resolve() {
	e, err := getEndpoints(k.k8sClient, k.target.serviceNamespace, k.target.serviceName)
	if err == nil {
		k.handle(e)
	} else {
		grpclog.Errorf("kuberesolver: lookup endpoints failed: %v", err)
	}
	// Next lookup should happen after an interval defined by k.freq.
	k.t.Reset(k.freq)
}

func (k *kResolver) watch() error {
	defer k.wg.Done()
	// watch endpoints lists existing endpoints at start
	sw, err := watchEndpoints(k.ctx, k.k8sClient, k.target.serviceNamespace, k.target.serviceName)
	if err != nil {
		return err
	}
	for {
		select {
		case <-k.ctx.Done():
			return nil
		case <-k.t.C:
			k.resolve()
		case <-k.rn:
			k.resolve()
		case up, hasMore := <-sw.ResultChan():
			if hasMore {
				k.handle(up.Object)
			} else {
				return nil
			}
		}
	}
}
