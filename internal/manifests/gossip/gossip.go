package gossip

import (
	"fmt"

	"github.com/openshift/loki-operator/internal/manifests"
)

func RingServiceURL(namespace string) string {
	return fmt.Sprintf(`%s.%s.svc.cluster.local:%d`, manifests.ServiceNameGossipRing, namespace, manifests.GossipPort)
}

func Labels() map[string]string {
	return map[string]string{
		"loki.grafana.com/gossip": "true",
	}
}
