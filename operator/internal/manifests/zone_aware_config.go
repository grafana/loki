package manifests

import (
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/imdario/mergo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	availabilityZoneEnvVarName    = "INSTANCE_AVAILABILITY_ZONE"
	concatenatedZonePodAnnotation = "metadata.annotations['loki_instance_availability_zone']"
	// labelZoneAwarePod is label to set on pods that have zone aware enabled
	labelZoneAwarePod       string = "loki.grafana.com/zoneaware"
	AnnotationLokiZoneAware string = "loki.grafana.com/zoneawareannotation"
)

// var zoneEnvNames = []string{}

func configureReplication(podSpec *corev1.PodSpec, replication lokiv1.ReplicationSpec, component string, name string) {
	podSpec.TopologySpreadConstraints = append(podSpec.TopologySpreadConstraints, topologySpreadConstraints(replication, component, name)...)
}

func configureZoneAwareEnv(podSpec *corev1.PodSpec, replication lokiv1.ReplicationSpec) error {
	if len(replication.Zones) > 0 {
		// set the Zone Aware Label on the pod
		// setZoneAwareLabel(name)
		resetEnvVar(podSpec, availabilityZoneEnvVarName)
		// podSpec.Containers[0].Env = append(podSpec.Containers[0].Env, getInstanceAvailabilityZoneEnvVar())

		src := corev1.Container{
			Env: getInstanceAvailabilityZoneEnvVar(),
		}

		for i, dst := range podSpec.Containers {
			if err := mergo.Merge(&dst, src, mergo.WithAppendSlice); err != nil {
				return err
			}
			podSpec.Containers[i] = dst
		}
	}
	return nil
}

// CheckZoneawareComponent returns true if the Component pod is either in the read/write path or gateway.
func CheckZoneawareComponent(pod *corev1.Pod) bool {
	podLabels := pod.Labels

	switch podLabels[kubernetesComponentLabel] {
	case LabelDistributorComponent, LabelIndexGatewayComponent,
		LabelIngesterComponent, LabelQuerierComponent,
		LabelQueryFrontendComponent:
		return true
	}
	return false
}

func getInstanceAvailabilityZoneEnvVar() []corev1.EnvVar {
	var envVars []corev1.EnvVar
	envVars = append(envVars,
		corev1.EnvVar{
			Name: availabilityZoneEnvVarName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: concatenatedZonePodAnnotation,
				},
			},
		},
	)
	return envVars
}

func topologySpreadConstraints(spec lokiv1.ReplicationSpec, component string, stackName string) []corev1.TopologySpreadConstraint {
	var tsc []corev1.TopologySpreadConstraint
	if len(spec.Zones) > 0 {
		tsc = make([]corev1.TopologySpreadConstraint, len(spec.Zones))
		for i, z := range spec.Zones {
			tsc[i] = corev1.TopologySpreadConstraint{
				MaxSkew:           int32(z.MaxSkew),
				TopologyKey:       z.TopologyKey,
				WhenUnsatisfiable: corev1.DoNotSchedule,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						kubernetesComponentLabel: component,
						kubernetesInstanceLabel:  stackName,
					},
				},
			}
		}
	}

	return tsc
}

// ComponentLabels is a list of all commonLabels including the app.kubernetes.io/component:<component> label
func setZoneAwareLabelAnnotation(zones []lokiv1.ZoneSpec, component, stackName, cfgSHA1, CertRotationRequiredAt string) (labels.Set, map[string]string) {
	var topologykey string
	for _, zone := range zones {
		if topologykey != "" {
			topologykey = topologykey + "," + zone.TopologyKey
		} else {
			topologykey = zone.TopologyKey
		}
	}
	l := labels.Merge(ComponentLabels(component, stackName), map[string]string{
		labelZoneAwarePod: "enabled",
	})
	a := commonAnnotations(cfgSHA1, CertRotationRequiredAt)
	a[AnnotationLokiZoneAware] = topologykey
	return l, a
}

// ComponentLabels is a list of all commonLabels including the app.kubernetes.io/component:<component> label
// func setZoneAwareAnnotation(component, stackName string, zones []lokiv1.ZoneSpec) labels.Set {
// 	var topologykey string
// 	for _, zone := range zones {
// 		if topologykey != "" {
// 			topologykey = topologykey + "-" + zone.TopologyKey
// 		} else {
// 			topologykey = zone.TopologyKey
// 		}
// 	}
// 	return labels.Merge(ComponentLabels(component, stackName), map[string]string{
// 		labelZoneAwarePod: topologykey,
// 	})
// }
