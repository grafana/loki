package openshift

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/ViaQ/logerr/v2/kverrors"
	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	manifests "github.com/grafana/loki/operator/internal/manifests/openshift"
)

var ErrInvalidVersionFormat = errors.New("invalid version format")

// FetchVersion queries the ClusterVersion CRD to get OpenShift version information.
// Returns (version, isOpenShift, error).
func FetchVersion(ctx context.Context, client client.Client) (*manifests.OpenShiftVersion, bool, error) {
	var clusterVersion configv1.ClusterVersion
	err := client.Get(ctx, types.NamespacedName{Name: "version"}, &clusterVersion)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, false, kverrors.Wrap(err, "failed to get ClusterVersion")
	}
	if clusterVersion.Name == "" || apierrors.IsNotFound(err) {
		// Not an OpenShift cluster
		return nil, false, nil
	}

	// Parse version from the ClusterVersion status
	if len(clusterVersion.Status.History) == 0 {
		return nil, false, kverrors.New("no version history found in ClusterVersion")
	}

	// Get the current version (first in history)
	var currentVersion string
	for _, history := range clusterVersion.Status.History {
		if history.State == configv1.CompletedUpdate {
			currentVersion = history.Version
			break
		}
	}
	if currentVersion == "" {
		return nil, false, kverrors.New("current version is empty in ClusterVersion")
	}

	version, err := parseVersion(currentVersion)
	if err != nil {
		return nil, false, kverrors.Wrap(err, "failed to parse version", "version", currentVersion)
	}

	return version, true, nil
}

// parseVersion parses a semantic version string into an OpenShiftVersion.
func parseVersion(versionStr string) (*manifests.OpenShiftVersion, error) {
	// Remove 'v' prefix if present
	versionStr = strings.TrimPrefix(versionStr, "v")

	// Split by dots
	parts := strings.Split(versionStr, ".")
	if len(parts) < 2 {
		return nil, kverrors.Wrap(ErrInvalidVersionFormat, "shorter than 2 parts", "version", versionStr)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, kverrors.Wrap(err, "invalid major version", "version", versionStr)
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, kverrors.Wrap(err, "invalid minor version", "version", versionStr)
	}

	return &manifests.OpenShiftVersion{
		Major: major,
		Minor: minor,
	}, nil
}
