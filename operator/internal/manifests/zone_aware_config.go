package manifests

import (
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/imdario/mergo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	availabilityZoneEnvVarName = "INSTANCE_AVAILABILITY_ZONE"
	availabilityZoneFieldPath  = "metadata.annotations['" + lokiv1.AnnotationAvailabilityZone + "']"
)

func configureReplication(podSpec *corev1.PodSpec, replication lokiv1.ReplicationSpec, component string, name string) {
	podSpec.TopologySpreadConstraints = append(podSpec.TopologySpreadConstraints, topologySpreadConstraints(replication, component, name)...)
}

func configureZoneAwareEnv(podSpec *corev1.PodSpec, replication lokiv1.ReplicationSpec) error {
	if len(replication.Zones) > 0 {
		// set the Zone Aware Label on the pod
		resetEnvVar(podSpec, availabilityZoneEnvVarName)

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

func getInstanceAvailabilityZoneEnvVar() []corev1.EnvVar {
	var envVars []corev1.EnvVar
	envVars = append(envVars,
		corev1.EnvVar{
			Name: availabilityZoneEnvVarName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: availabilityZoneFieldPath,
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
		lokiv1.LabelZoneAwarePod: "enabled",
	})
	a := commonAnnotations(cfgSHA1, CertRotationRequiredAt)
	a[lokiv1.AnnotationAvailabilityZoneLabels] = topologykey
	return l, a
}
