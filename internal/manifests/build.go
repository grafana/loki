package manifests

import (
	"github.com/ViaQ/logerr/kverrors"
	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/manifests/internal"

	"github.com/imdario/mergo"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildAll builds all manifests required to run a Loki Stack
func BuildAll(opt Options) ([]client.Object, error) {
	res := make([]client.Object, 0)

	opt, err := applyUserOptions(opt)
	if err != nil {
		return nil, err
	}

	cm, sha1, err := LokiConfigMap(opt)
	if err != nil {
		return nil, err
	}
	opt.ConfigSHA1 = sha1

	res = append(res, cm)
	res = append(res, BuildDistributor(opt)...)
	res = append(res, BuildIngester(opt)...)
	res = append(res, BuildQuerier(opt)...)
	res = append(res, BuildCompactor(opt)...)
	res = append(res, BuildQueryFrontend(opt)...)
	res = append(res, LokiGossipRingService(opt.Name))

	return res, nil
}

func applyUserOptions(opt Options) (Options, error) {
	defs := internal.StackSizeTable[opt.Stack.Size]
	spec := (&defs).DeepCopy()
	if err := mergo.Merge(spec, opt.Stack, mergo.WithOverride); err != nil {
		return Options{}, kverrors.Wrap(err, "failed merging stack user options", "name", opt.Name)
	}

	strictOverrides := lokiv1beta1.LokiStackSpec{
		Template: &lokiv1beta1.LokiTemplateSpec{
			Compactor: &lokiv1beta1.LokiComponentSpec{
				// Compactor is a singelton application.
				// Only one replica allowed!!!
				Replicas: 1,
			},
		},
	}
	if err := mergo.Merge(spec, strictOverrides, mergo.WithOverride); err != nil {
		return Options{}, kverrors.Wrap(err, "failed to merge strict defaults")
	}

	opt.Stack = *spec
	return opt, nil
}
