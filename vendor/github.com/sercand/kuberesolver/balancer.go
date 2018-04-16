package kuberesolver

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/naming"
)

type Balancer struct {
	Namespace string
	client    *k8sClient
	resolvers []*kubeResolver
}

type TargetUrlType int32

const (
	TargetTypeDNS        TargetUrlType = 0
	TargetTypeKubernetes TargetUrlType = 1
	kubernetesSchema                   = "kubernetes"
	dnsSchema                          = "dns"
)

type targetInfo struct {
	urlType           TargetUrlType
	target            string
	port              string
	resolveByPortName bool
	useFirstPort      bool
}

func parseTarget(target string) (targetInfo, error) {
	u, err := url.Parse(target)
	if err != nil {
		return targetInfo{}, err
	}
	ti := targetInfo{}
	if u.Scheme == kubernetesSchema {
		ti.urlType = TargetTypeKubernetes
		spl := strings.Split(u.Host, ":")
		if len(spl) == 2 {
			ti.target = spl[0]
			ti.port = spl[1]
			ti.useFirstPort = false
			if _, err := strconv.Atoi(ti.port); err != nil {
				ti.resolveByPortName = true
			} else {
				ti.resolveByPortName = false
			}
		} else {
			ti.target = spl[0]
			ti.useFirstPort = true
		}
	} else if u.Scheme == dnsSchema {
		ti.urlType = TargetTypeDNS
		ti.target = u.Host
	} else {
		ti.urlType = TargetTypeDNS
		ti.target = target
	}
	return ti, nil
}

//Resolver returns Resolver for grpc
func (b *Balancer) Resolver() naming.Resolver {
	return newResolver(b.client, b.Namespace)
}

//DialOption returns grpc.DialOption with RoundRobin balancer and resolver
func (b *Balancer) DialOption() grpc.DialOption {
	rs := newResolver(b.client, b.Namespace)
	return grpc.WithBalancer(grpc.RoundRobin(rs))
}

// Dial calls grpc.Dial, also parses target and uses load balancer if necessary
func (b *Balancer) Dial(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	pt, err := parseTarget(target)
	if err != nil {
		return nil, err
	}
	switch pt.urlType {
	case TargetTypeKubernetes:
		if b.client == nil {
			return nil, errors.New("application is not running inside kubernetes")
		}
		grpclog.Printf("kuberesolver: using kubernetes resolver target=%s", pt.target)
		rs := newResolver(b.client, b.Namespace)
		b.resolvers = append(b.resolvers, rs)
		opts := append(opts, grpc.WithBalancer(grpc.RoundRobin(rs)))
		return grpc.Dial(target, opts...)
	case TargetTypeDNS:
		return grpc.Dial(pt.target, opts...)
	default:
		return nil, errors.New("Unknown target type")
	}
}

func (b *Balancer) Healthy() error {
	for _, r := range b.resolvers {
		if r.watcher != nil {
			if len(r.watcher.endpoints) == 0 {
				return fmt.Errorf("target does not have endpoints")
			}
		}
	}
	return nil
}

// IsInCluster returns true if application is running inside kubernetes cluster
func (b *Balancer) IsInCluster() bool {
	return b.client != nil
}

// New creates a Balancer with "default" namespace
func New() *Balancer {
	return NewWithNamespace("default")
}

// NewWithNamespace creates a Balancer with given namespace.
func NewWithNamespace(namespace string) *Balancer {
	client, err := newInClusterClient()
	if err != nil {
		grpclog.Printf("kuberesolver: application is not running inside kubernetes")
	}
	return &Balancer{
		Namespace: namespace,
		client:    client,
	}
}
