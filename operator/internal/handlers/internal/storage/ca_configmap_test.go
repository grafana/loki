package storage_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/grafana/loki/operator/internal/handlers/internal/storage"
)

func TestIsValidConfigMap(t *testing.T) {
	type test struct {
		name  string
		cm    *corev1.ConfigMap
		valid bool
	}
	table := []test{
		{
			name: "valid CA configmap",
			cm: &corev1.ConfigMap{
				Data: map[string]string{
					"service-ca.crt": "has-some-data",
				},
			},
			valid: true,
		},
		{
			name: "missing `service-ca.crt` key",
			cm:   &corev1.ConfigMap{},
		},
		{
			name: "missing CA content",
			cm: &corev1.ConfigMap{
				Data: map[string]string{
					"service-ca.crt": "",
				},
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			ok := storage.IsValidCAConfigMap(tst.cm, "service-ca.crt")
			require.Equal(t, tst.valid, ok)
		})
	}
}
