package manifests

import (
	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func configurePodSpecForRestrictedStandard(podSpec *corev1.PodSpec) error {
	podSecurityContext := corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr.To(true),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
	}

	containerSecurityContext := corev1.Container{
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}

	for i, container := range podSpec.Containers {
		if err := mergo.Merge(&container, containerSecurityContext, mergo.WithOverride); err != nil {
			return err
		}
		podSpec.Containers[i] = container
	}

	if err := mergo.Merge(podSpec, podSecurityContext, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge pod security context")
	}

	return nil
}
