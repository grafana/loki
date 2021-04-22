package manifests

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildAll builds all manifests required to run a Loki Stack
func BuildAll(opt Options) ([]client.Object, error) {
	res := make([]client.Object, 0)

	cm, err := LokiConfigMap(opt)
	if err != nil {
		return nil, err
	}

	res = append(res, cm)
	res = append(res, BuildDistributor(opt)...)

	ingesterObjects, err := BuildIngester(opt)
	if err != nil {
		return nil, err
	}
	res = append(res, ingesterObjects...)

	querierObjects, err := BuildQuerier(opt)
	if err != nil {
		return nil, err
	}
	res = append(res, querierObjects...)

	res = append(res, BuildCompactor(opt)...)
	res = append(res, BuildQueryFrontend(opt)...)
	res = append(res, LokiGossipRingService(opt.Name))

	return res, nil
}
