package manifests

import (
	"strings"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	availabilityZoneEnvVarName = "INSTANCE_AVAILABILITY_ZONE"
	availabilityZoneFieldPath  = "metadata.annotations['" + lokiv1.AnnotationAvailabilityZone + "']"
)

var availabilityZoneEnvVar = corev1.EnvVar{
	Name: availabilityZoneEnvVarName,
	ValueFrom: &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{
			FieldPath: availabilityZoneFieldPath,
		},
	},
}

func configureReplication(podTemplate *corev1.PodTemplateSpec, replication *lokiv1.ReplicationSpec, component string, stackName string) {
	zoneKeys := []string{}
	for _, zone := range replication.Zones {
		zoneKeys = append(zoneKeys, zone.TopologyKey)
		podTemplate.Spec.TopologySpreadConstraints = append(podTemplate.Spec.TopologySpreadConstraints, corev1.TopologySpreadConstraint{
			MaxSkew:           int32(zone.MaxSkew),
			TopologyKey:       zone.TopologyKey,
			WhenUnsatisfiable: corev1.DoNotSchedule,
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					kubernetesComponentLabel: component,
					kubernetesInstanceLabel:  stackName,
				},
			},
		})
	}
	topologyKey := strings.Join(zoneKeys, ",")

	for i := range podTemplate.Spec.Containers {
		podTemplate.Spec.Containers[i].Env = append(podTemplate.Spec.Containers[i].Env, availabilityZoneEnvVar)
	}

	podTemplate.Labels = labels.Merge(podTemplate.Labels, map[string]string{
		lokiv1.LabelZoneAwarePod: "enabled",
	})
	podTemplate.Annotations[lokiv1.AnnotationAvailabilityZoneLabels] = topologyKey
}
