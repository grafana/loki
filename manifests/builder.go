package manifests

import (
	apps "k8s.io/api/apps/v1"
)

type Options struct {
}

// DefaultOptions needs no comment
func DefaultOptions() Options {
	return Options{}
}

// Build creates a list of manifests that should be deployed
func Build(_ Options) ([]apps.Deployment, error) {
	dep, err := DistributorDeployment()
	return []apps.Deployment{dep}, err
}
