package openshift

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/ViaQ/logerr/v2/kverrors"
	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	manifests "github.com/grafana/loki/operator/internal/manifests/openshift"
)

var ErrInvalidVersionFormat = errors.New("invalid version format")

// FetchVersion queries the ClusterVersion CRD to get OpenShift version information.
// Returns (version, isOpenShift, error).
func FetchVersion(ctx context.Context, client client.Client) (*manifests.OpenShiftRelease, error) {
	var clusterVersion configv1.ClusterVersion
	err := client.Get(ctx, types.NamespacedName{Name: "version"}, &clusterVersion)
	if err != nil {
		if meta.IsNoMatchError(err) || runtime.IsNotRegisteredError(err) || apierrors.IsNotFound(err) {
			return nil, nil // Not an OpenShift cluster
		}
		return nil, kverrors.Wrap(err, "failed to get ClusterVersion")
	}
	if clusterVersion.Name == "" {
		return nil, nil // Not an OpenShift cluster
	}

	if len(clusterVersion.Status.History) == 0 {
		return nil, kverrors.New("no version history found in ClusterVersion")
	}

	currentVersion := clusterVersion.Status.Desired.Version // default to desired version if no history is found
	for _, history := range clusterVersion.Status.History {
		if history.State == configv1.CompletedUpdate {
			currentVersion = history.Version
			break
		}
	}
	if currentVersion == "" {
		return nil, kverrors.New("was not able to determine OpenShift version")
	}

	version, err := parseVersion(currentVersion)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to parse version", "version", currentVersion)
	}

	return version, nil
}

func parseVersion(versionStr string) (*manifests.OpenShiftRelease, error) {
	versionStr = strings.TrimPrefix(versionStr, "v")

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

	return &manifests.OpenShiftRelease{
		Major: major,
		Minor: minor,
	}, nil
}
