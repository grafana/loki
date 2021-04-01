package manifests

import (
	"github.com/ViaQ/logerr/kverrors"
	"github.com/creasty/defaults"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildAll builds all manifests required to run a Loki Stack
func BuildAll(opt Options) ([]client.Object, error) {
	if err := setDefaultOptions(&opt); err != nil {
		return nil, err
	}

	res := make([]client.Object, 0)

	cm, err := LokiConfigMap(opt.Name, opt.Namespace)
	if err != nil {
		return nil, err
	}

	res = append(res, cm)
	res = append(res, BuildDistributor(opt.Name)...)

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

	res = append(res, BuildQueryFrontend(opt.Name)...)
	res = append(res, LokiGossipRingService(opt.Name))

	return res, nil
}

func setDefaultOptions(o *Options) error {
	if err := defaults.Set(o); err != nil {
		// I believe this is only caused when the default tag has an incorrect type such as Field int `default:"hello"`
		// so it shouldn't happen in production
		return kverrors.Wrap(err, "could not set default options")
	}
	// TODO add some complex defaults here that the vendored package does not support
	return nil
}
