package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestCheckValidConfigMap(t *testing.T) {
	type test struct {
		name         string
		cm           *corev1.ConfigMap
		wantHash     string
		wantErrorMsg string
	}
	table := []test{
		{
			name: "valid CA configmap",
			cm: &corev1.ConfigMap{
				Data: map[string]string{
					"service-ca.crt": "has-some-data",
				},
			},
			wantHash:     "de6ae206d4920549d21c24ad9721e87a9b1ec7dc",
			wantErrorMsg: "",
		},
		{
			name:         "missing `service-ca.crt` key",
			cm:           &corev1.ConfigMap{},
			wantErrorMsg: "key not present or data empty: service-ca.crt",
		},
		{
			name: "missing CA content",
			cm: &corev1.ConfigMap{
				Data: map[string]string{
					"service-ca.crt": "",
				},
			},
			wantErrorMsg: "key not present or data empty: service-ca.crt",
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			hash, err := checkCAConfigMap(tst.cm, "service-ca.crt")

			require.Equal(t, tst.wantHash, hash)
			if tst.wantErrorMsg == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tst.wantErrorMsg)
			}
		})
	}
}
