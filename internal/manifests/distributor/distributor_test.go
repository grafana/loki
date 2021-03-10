package distributor_test

import (
	"testing"

	"github.com/openshift/loki-operator/internal/manifests/distributor"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestWorks(t *testing.T) {
	ns := "mynamespace"
	name := "myname"
	d, err := distributor.New(ns, name, 1)
	require.NoError(t, err)

	deploy, err := yaml.Marshal(d.Deployment())
	require.NoError(t, err)
	_ = deploy
}
