package distributor

import (
	"github.com/openshift/loki-operator/internal/manifests"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func services(namespace string) []core.Service {
	return []core.Service{
		{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: apps.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      manifests.ServiceNameDistributorHTTP,
				Namespace: namespace,
				Labels:    commonLabels,
			},
			Spec: core.ServiceSpec{
				ClusterIP: "None",
				Ports: []core.ServicePort{
					{
						Name:     "http",
						Port:     manifests.MetricsPort,
						Protocol: "TCP",
					},
				},
				Selector: commonLabels,
			},
			Status: core.ServiceStatus{},
		},
		{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: apps.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      manifests.ServiceNameDistributorGRPC,
				Namespace: namespace,
				Labels:    commonLabels,
			},
			Spec: core.ServiceSpec{
				ClusterIP: "None",
				Ports: []core.ServicePort{
					{
						Name: "grpc",
						Port: manifests.GRPCPort,
					},
				},
				Selector: commonLabels,
			},
		},
	}
}
