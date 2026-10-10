package certrotation

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
)

const openshiftCertAlertThreshold = 30 * 24 * time.Hour

func TestOpenShiftOverlayTargetCertRefreshLeavesMoreThan30DaysRemaining(t *testing.T) {
	t.Parallel()

	overlays := []string{
		"openshift",
		"community-openshift",
	}

	for _, overlay := range overlays {
		t.Run(overlay, func(t *testing.T) {
			t.Parallel()

			cfgPath := filepath.Join("..", "..", "config", "overlays", overlay, "controller_manager_config.yaml")
			data, err := os.ReadFile(cfgPath)
			require.NoError(t, err)

			var cfg configv1.ProjectConfig
			require.NoError(t, yaml.Unmarshal(data, &cfg))

			certMgmt := cfg.Gates.BuiltInCertManagement
			require.True(t, certMgmt.Enabled)

			rotation, err := ParseRotation(certMgmt)
			require.NoError(t, err)

			remaining := rotation.TargetCertValidity - rotation.TargetCertRefresh
			require.Greater(t, remaining, openshiftCertAlertThreshold,
				"target cert refresh must leave more than 30 days before expiry to avoid false-positive OpenShift alerts")
		})
	}
}
