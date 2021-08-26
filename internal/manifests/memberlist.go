package manifests

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/intstr"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildLokiGossipRingService creates a k8s service for the gossip/memberlist members of the cluster
func BuildLokiGossipRingService(stackName string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("loki-gossip-ring-%s", stackName),
			Labels: commonLabels(stackName),
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{
					Name:       "gossip",
					Port:       gossipPort,
					Protocol:   protocolTCP,
					TargetPort: intstr.IntOrString{IntVal: gossipPort},
				},
			},
			Selector: commonLabels(stackName),
		},
	}
}
