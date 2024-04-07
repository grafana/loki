package test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

type replicas struct {
	Replicas int `yaml:"replicas"`
}
type loki struct {
	Storage struct {
		Type string `yaml:"type"`
	} `yaml:"storage"`
}

type values struct {
	DeploymentMode string   `yaml:"deploymentMode"`
	Backend        replicas `yaml:"backend"`
	Compactor      replicas `yaml:"compactor"`
	Distributor    replicas `yaml:"distributor"`
	IndexGateway   replicas `yaml:"indexGateway"`
	Ingester       replicas `yaml:"ingester"`
	Querier        replicas `yaml:"querier"`
	QueryFrontend  replicas `yaml:"queryFrontend"`
	QueryScheduler replicas `yaml:"queryScheduler"`
	Read           replicas `yaml:"read"`
	Ruler          replicas `yaml:"ruler"`
	SingleBinary   replicas `yaml:"singleBinary"`
	Write          replicas `yaml:"write"`

	Loki loki `yaml:"loki"`
}

func templateConfig(t *testing.T, vals values) error {
	y, err := yaml.Marshal(&vals)
	require.NoError(t, err)
	require.Greater(t, len(y), 0)

	f, err := os.CreateTemp("", "values.yaml")
	require.NoError(t, err)

	_, err = f.Write(y)
	require.NoError(t, err)

	cmd := exec.Command("helm", "dependency", "build")
	// Dependency build needs to be run from the parent directory where the chart is located.
	cmd.Dir = "../"
	var cmdOutput []byte
	if cmdOutput, err = cmd.CombinedOutput(); err != nil {
		t.Log("dependency build failed", "err", string(cmdOutput))
		return err
	}

	cmd = exec.Command("helm", "template", "../", "--values", f.Name())
	if cmdOutput, err := cmd.CombinedOutput(); err != nil {
		t.Log("template failed", "err", string(cmdOutput))
		return err
	}

	return nil
}

// E.Welch these tests fail because the templateConfig function above can't resolve the chart dependencies and I'm not sure how to fix this....

//func Test_InvalidConfigs(t *testing.T) {
//	t.Run("running both single binary and scalable targets", func(t *testing.T) {
//		vals := values{
//			SingleBinary: replicas{Replicas: 1},
//			Write:        replicas{Replicas: 1},
//			Loki: loki{
//				Storage: struct {
//					Type string `yaml:"type"`
//				}{Type: "gcs"},
//			},
//		}
//		require.Error(t, templateConfig(t, vals))
//	})
//
//	t.Run("running both single binary and distributed targets", func(t *testing.T) {
//		vals := values{
//			SingleBinary: replicas{Replicas: 1},
//			Distributor:  replicas{Replicas: 1},
//			Loki: loki{
//				Storage: struct {
//					Type string `yaml:"type"`
//				}{Type: "gcs"},
//			},
//		}
//		require.Error(t, templateConfig(t, vals))
//	})
//
//	t.Run("running both scalable and distributed targets", func(t *testing.T) {
//		vals := values{
//			Read:        replicas{Replicas: 1},
//			Distributor: replicas{Replicas: 1},
//			Loki: loki{
//				Storage: struct {
//					Type string `yaml:"type"`
//				}{Type: "gcs"},
//			},
//		}
//		require.Error(t, templateConfig(t, vals))
//	})
//
//	t.Run("running scalable with filesystem storage", func(t *testing.T) {
//		vals := values{
//			Read: replicas{Replicas: 1},
//			Loki: loki{
//				Storage: struct {
//					Type string `yaml:"type"`
//				}{Type: "filesystem"},
//			},
//		}
//
//		require.Error(t, templateConfig(t, vals))
//	})
//
//	t.Run("running distributed with filesystem storage", func(t *testing.T) {
//		vals := values{
//			Distributor: replicas{Replicas: 1},
//			Loki: loki{
//				Storage: struct {
//					Type string `yaml:"type"`
//				}{Type: "filesystem"},
//			},
//		}
//
//		require.Error(t, templateConfig(t, vals))
//	})
//}
//
//func Test_ValidConfigs(t *testing.T) {
//	t.Run("single binary", func(t *testing.T) {
//		vals := values{
//
//			DeploymentMode: "SingleBinary",
//
//			SingleBinary: replicas{Replicas: 1},
//
//			Backend:        replicas{Replicas: 0},
//			Compactor:      replicas{Replicas: 0},
//			Distributor:    replicas{Replicas: 0},
//			IndexGateway:   replicas{Replicas: 0},
//			Ingester:       replicas{Replicas: 0},
//			Querier:        replicas{Replicas: 0},
//			QueryFrontend:  replicas{Replicas: 0},
//			QueryScheduler: replicas{Replicas: 0},
//			Read:           replicas{Replicas: 0},
//			Ruler:          replicas{Replicas: 0},
//			Write:          replicas{Replicas: 0},
//
//			Loki: loki{
//				Storage: struct {
//					Type string `yaml:"type"`
//				}{Type: "filesystem"},
//			},
//		}
//		require.NoError(t, templateConfig(t, vals))
//	})
//
//	t.Run("scalable", func(t *testing.T) {
//		vals := values{
//
//			DeploymentMode: "SimpleScalable",
//
//			Backend: replicas{Replicas: 1},
//			Read:    replicas{Replicas: 1},
//			Write:   replicas{Replicas: 1},
//
//			Compactor:      replicas{Replicas: 0},
//			Distributor:    replicas{Replicas: 0},
//			IndexGateway:   replicas{Replicas: 0},
//			Ingester:       replicas{Replicas: 0},
//			Querier:        replicas{Replicas: 0},
//			QueryFrontend:  replicas{Replicas: 0},
//			QueryScheduler: replicas{Replicas: 0},
//			Ruler:          replicas{Replicas: 0},
//			SingleBinary:   replicas{Replicas: 0},
//
//			Loki: loki{
//				Storage: struct {
//					Type string `yaml:"type"`
//				}{Type: "gcs"},
//			},
//		}
//		require.NoError(t, templateConfig(t, vals))
//	})
//
//	t.Run("distributed", func(t *testing.T) {
//		vals := values{
//			DeploymentMode: "Distributed",
//
//			Compactor:      replicas{Replicas: 1},
//			Distributor:    replicas{Replicas: 1},
//			IndexGateway:   replicas{Replicas: 1},
//			Ingester:       replicas{Replicas: 1},
//			Querier:        replicas{Replicas: 1},
//			QueryFrontend:  replicas{Replicas: 1},
//			QueryScheduler: replicas{Replicas: 1},
//			Ruler:          replicas{Replicas: 1},
//
//			Backend:      replicas{Replicas: 0},
//			Read:         replicas{Replicas: 0},
//			SingleBinary: replicas{Replicas: 0},
//			Write:        replicas{Replicas: 0},
//
//			Loki: loki{
//				Storage: struct {
//					Type string `yaml:"type"`
//				}{Type: "gcs"},
//			},
//		}
//		require.NoError(t, templateConfig(t, vals))
//	})
//}
