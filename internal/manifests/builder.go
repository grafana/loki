package manifests

import (
	"os"
	"path"

	"github.com/ViaQ/logerr/kverrors"
	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resource"
)

type Options struct {
	// TODO options should be provided by the user or environment
}

// DefaultOptions needs no comment
func DefaultOptions() Options {
	return Options{}
}

// Build creates a list of manifests that should be deployed
func Build(Options) ([]*resource.Resource, error) {
	fs := filesys.MakeFsOnDisk()
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	exe, _ := os.Executable()
	workdir := path.Dir(exe)
	kpath := path.Join(workdir, "/kustomize/v1/overlays/xs")

	res, err := k.Run(fs, kpath)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to build manifests", "workdir", kpath)
	}
	return res.Resources(), nil
}
