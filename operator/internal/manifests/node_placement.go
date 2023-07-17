package manifests

import (
	"fmt"
	"strings"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/imdario/mergo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	availabilityZoneEnvVarName = "INSTANCE_AVAILABILITY_ZONE"
	availabilityZoneFieldPath  = "metadata.annotations['" + lokiv1.AnnotationAvailabilityZone + "']"
	// availabilityZoneVolumeName is the name of the volume that will contain the
	// availability zone annotation we get from DownwardAPI
	availabilityZoneVolumeName = "az-annotation"
	// availabilityZoneVolumeMountPath path where the volume will be mounted on the init container
	availabilityZoneVolumeMountPath = "/etc/az-annotation"
	// availabilityZoneVolumeFileName name of the file containing the availability zone annotation
	availabilityZoneVolumeFileName = "az"
)

var availabilityZoneEnvVar = corev1.EnvVar{
	Name: availabilityZoneEnvVarName,
	ValueFrom: &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{
			FieldPath: availabilityZoneFieldPath,
		},
	},
}

func configureReplication(podTemplate *corev1.PodTemplateSpec, replication *lokiv1.ReplicationSpec, component string, stackName string) error {
	if replication == nil || len(replication.Zones) == 0 {
		return nil
	}

	template := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				lokiv1.LabelZoneAwarePod: "enabled",
			},
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{
			Containers: make([]corev1.Container, len(podTemplate.Spec.Containers)),
		},
	}

	zoneKeys := []string{}
	for _, zone := range replication.Zones {
		zoneKeys = append(zoneKeys, zone.TopologyKey)
		template.Spec.TopologySpreadConstraints = append(template.Spec.TopologySpreadConstraints, corev1.TopologySpreadConstraint{
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
	template.Annotations[lokiv1.AnnotationAvailabilityZoneLabels] = topologyKey

	if component != LabelGatewayComponent {
		template.Spec.InitContainers = []corev1.Container{
			initContainerAZAnnotationCheck(podTemplate.Spec.Containers[0].Image),
		}

		src := corev1.Container{
			Env: []corev1.EnvVar{availabilityZoneEnvVar},
		}

		for i, dst := range podTemplate.Spec.Containers {
			if err := mergo.Merge(&dst, src, mergo.WithAppendSlice); err != nil {
				return err
			}
			podTemplate.Spec.Containers[i] = dst
		}

		vols := []corev1.Volume{azAnnotationVolume()}
		if err := mergo.Merge(&podTemplate.Spec.Volumes, vols, mergo.WithAppendSlice); err != nil {
			return err
		}
	}

	if err := mergo.Merge(podTemplate, template); err != nil {
		return err
	}

	return nil
}

func initContainerAZAnnotationCheck(image string) corev1.Container {
	azPath := fmt.Sprintf("%s/%s", availabilityZoneVolumeMountPath, availabilityZoneVolumeFileName)
	return corev1.Container{
		Name:  "az-annotation-check",
		Image: image,
		Command: []string{
			"sh",
			"-c",
			fmt.Sprintf("while ! [ -s %s ]; do echo Waiting for availability zone annotation to be set; sleep 2; done; echo availability zone annotation is set; cat %s", azPath, azPath),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      availabilityZoneVolumeName,
				MountPath: availabilityZoneVolumeMountPath,
			},
		},
	}
}

func azAnnotationVolume() corev1.Volume {
	return corev1.Volume{
		Name: availabilityZoneVolumeName,
		VolumeSource: corev1.VolumeSource{
			DownwardAPI: &corev1.DownwardAPIVolumeSource{
				Items: []corev1.DownwardAPIVolumeFile{
					{
						Path: availabilityZoneVolumeFileName,
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: availabilityZoneFieldPath,
						},
					},
				},
			},
		},
	}
}
