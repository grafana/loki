package manifests

import (
	"embed"
	"io/fs"

	"github.com/ViaQ/logerr/kverrors"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

//go:embed 0.0.1/*
var embededFS embed.FS

func DistributorDeployment() (res apps.Deployment, err error) {
	err = readFile(&res, "0.0.1/base/distributor-deploy.yaml")
	return
}

func DistributorGrpcService() (res core.Service, err error) {
	err = readFile(&res, "0.0.1/base/distributor-grpc-service.yaml")
	return
}

func readFile(to interface{}, path string) error {
	b, err := fs.ReadFile(embededFS, path)
	if err != nil {
		return kverrors.Wrap(err, "failed to read file", "path", path)
	}

	if err := yaml.Unmarshal(b, to); err != nil {
		return kverrors.Wrap(err, "file was in the incorrect format", "path", path)
	}
	return nil
}
