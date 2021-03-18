package manifests

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildAll builds all manifests required to run a Loki Stack
// TODO add options parameter to enable resource sizing, and other configurations
func BuildAll(stackName, namespace string) ([]client.Object, error) {
	res := make([]client.Object, 0)

	cm, err := LokiConfigMap(stackName, namespace)
	if err != nil {
		return nil, err
	}
	res = append(res, cm)
	res = append(res, BuildDistributor(stackName)...)
	res = append(res, BuildIngester(stackName)...)
	res = append(res, BuildQuerier(stackName)...)
	res = append(res, BuildQueryFrontend(stackName)...)
	res = append(res, LokiGossipRingService(stackName))

	return res, nil
}
