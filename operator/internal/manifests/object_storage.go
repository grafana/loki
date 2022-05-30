package manifests

import (
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
	appsv1 "k8s.io/api/apps/v1"
)

func configureDeploymentForStorageSpec(
	d *appsv1.Deployment,
	s *lokiv1beta1.ObjectStorageSpec,
) error {
	switch s.Secret.Type {
	case lokiv1beta1.ObjectStorageSecretGCS:
		return storage.ConfigureDeployment(d, s.Secret.Name)
	case lokiv1beta1.ObjectStorageSecretS3:
		if s.TLS == nil {
			return nil
		}

		return storage.ConfigureDeploymentCA(d, s.TLS.CA)
	default:
		return nil
	}
}

func configureStatefulSetForStorageSpec(
	d *appsv1.StatefulSet,
	s *lokiv1beta1.ObjectStorageSpec,
) error {
	switch s.Secret.Type {
	case lokiv1beta1.ObjectStorageSecretGCS:
		return storage.ConfigureStatefulSet(d, s.Secret.Name)
	case lokiv1beta1.ObjectStorageSecretS3:
		if s.TLS == nil {
			return nil
		}

		return storage.ConfigureStatefulSetCA(d, s.TLS.CA)
	default:
		return nil
	}
}
