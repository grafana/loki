package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/status"
)

var (
	featureGates = configv1.FeatureGates{
		ServiceMonitors:            false,
		ServiceMonitorTLSEndpoints: false,
		BuiltInCertManagement: configv1.BuiltInCertManagement{
			Enabled:        true,
			CACertValidity: "10m",
			CACertRefresh:  "5m",
			CertValidity:   "2m",
			CertRefresh:    "1m",
		},
	}

	defaultSecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-stack-secret",
			Namespace: "some-ns",
		},
		Data: map[string][]byte{
			"endpoint":          []byte("s3://your-endpoint"),
			"region":            []byte("a-region"),
			"bucketnames":       []byte("bucket1,bucket2"),
			"access_key_id":     []byte("a-secret-id"),
			"access_key_secret": []byte("a-secret-key"),
		},
	}

	invalidSecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-stack-secret",
			Namespace: "some-ns",
		},
		Data: map[string][]byte{},
	}

	invalidCAConfigMap = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-stack-ca-configmap",
			Namespace: "some-ns",
		},
		Data: map[string]string{},
	}
)

func TestBuildOptions_WhenMissingSecret_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Missing object storage secret",
		Reason:  lokiv1.ReasonMissingObjectStorageSecret,
		Requeue: false,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		_, isLokiStack := object.(*lokiv1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && isLokiStack {
			k.SetClientObject(object, stack)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	_, err := BuildOptions(context.TODO(), k, stack, featureGates)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestBuildOptions_WhenInvalidSecret_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid object storage secret contents: missing secret field",
		Reason:  lokiv1.ReasonInvalidObjectStorageSecret,
		Requeue: false,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: invalidSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		_, isLokiStack := object.(*lokiv1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && isLokiStack {
			k.SetClientObject(object, stack)
			return nil
		}
		if name.Name == invalidSecret.Name {
			k.SetClientObject(object, &invalidSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	_, err := BuildOptions(context.TODO(), k, stack, featureGates)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestBuildOptions_WithInvalidStorageSchema_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid object storage schema contents: spec does not contain any schemas",
		Reason:  lokiv1.ReasonInvalidObjectStorageSchema,
		Requeue: false,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
		},
		Status: lokiv1.LokiStackStatus{
			Storage: lokiv1.LokiStackStorageStatus{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
					{
						Version:       lokiv1.ObjectStorageSchemaV12,
						EffectiveDate: "2021-10-11",
					},
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		_, isLokiStack := object.(*lokiv1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && isLokiStack {
			k.SetClientObject(object, stack)
			return nil
		}
		if name.Name == defaultSecret.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	_, err := BuildOptions(context.TODO(), k, stack, featureGates)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestBuildOptions_WhenMissingCAConfigMap_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Missing object storage CA config map",
		Reason:  lokiv1.ReasonMissingObjectStorageCAConfigMap,
		Requeue: false,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
				TLS: &lokiv1.ObjectStorageTLSSpec{
					CASpec: lokiv1.CASpec{
						CA: "not-existing",
					},
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		_, isLokiStack := object.(*lokiv1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && isLokiStack {
			k.SetClientObject(object, stack)
			return nil
		}

		if name.Name == defaultSecret.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}

		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	_, err := BuildOptions(context.TODO(), k, stack, featureGates)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestBuildOptions_WhenEmptyCAConfigMapName_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Missing object storage CA config map",
		Reason:  lokiv1.ReasonMissingObjectStorageCAConfigMap,
		Requeue: false,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
				TLS: &lokiv1.ObjectStorageTLSSpec{
					CASpec: lokiv1.CASpec{
						CA: "",
					},
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		_, isLokiStack := object.(*lokiv1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && isLokiStack {
			k.SetClientObject(object, stack)
			return nil
		}

		if name.Name == defaultSecret.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}

		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	_, err := BuildOptions(context.TODO(), k, stack, featureGates)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestBuildOptions_WhenInvalidCAConfigMap_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid object storage CA configmap contents: key not present or data empty: service-ca.crt",
		Reason:  lokiv1.ReasonInvalidObjectStorageCAConfigMap,
		Requeue: false,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
				TLS: &lokiv1.ObjectStorageTLSSpec{
					CASpec: lokiv1.CASpec{
						CA: invalidCAConfigMap.Name,
					},
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		_, isLokiStack := object.(*lokiv1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && isLokiStack {
			k.SetClientObject(object, stack)
			return nil
		}
		if name.Name == defaultSecret.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}

		if name.Name == invalidCAConfigMap.Name {
			k.SetClientObject(object, &invalidCAConfigMap)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	_, err := BuildOptions(context.TODO(), k, stack, featureGates)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}
