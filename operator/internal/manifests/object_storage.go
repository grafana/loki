package manifests

import (
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
	appsv1 "k8s.io/api/apps/v1"
)

func configureDeploymentForStorageType(
	d *appsv1.Deployment,
	t lokiv1beta1.ObjectStorageSecretType,
	secretName string) error {
	switch t {
	case lokiv1beta1.ObjectStorageSecretGCS:
		return storage.ConfigureDeployment(d, secretName)
	default:
		return nil
	}
}

func configureStatefulSetForStorageType(
	d *appsv1.StatefulSet,
	t lokiv1beta1.ObjectStorageSecretType,
	secretName string) error {
	switch t {
	case lokiv1beta1.ObjectStorageSecretGCS:
		return storage.ConfigureStatefulSet(d, secretName)
	default:
		return nil
	}
}
