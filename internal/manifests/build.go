package manifests

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildAll builds all manifests required to run a Loki Stack
func BuildAll(opt Options) ([]client.Object, error) {
	res := make([]client.Object, 0)

	cm, err := LokiConfigMap(opt.Name, opt.Namespace)
	if err != nil {
		return nil, err
	}
	res = append(res, cm)
	res = append(res, BuildDistributor(opt.Name)...)
	res = append(res, BuildIngester(opt.Name)...)
	res = append(res, BuildQuerier(opt.Name)...)
	res = append(res, BuildQueryFrontend(opt.Name)...)
	res = append(res, LokiGossipRingService(opt.Name))

	return res, nil
}
