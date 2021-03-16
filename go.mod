module github.com/ViaQ/loki-operator

go 1.16

require (
	github.com/ViaQ/logerr v1.0.9
	github.com/go-logr/logr v0.4.0
	github.com/onsi/ginkgo v1.15.1
	github.com/onsi/gomega v1.11.0
	k8s.io/api v0.20.4
	k8s.io/apimachinery v0.20.4
	k8s.io/client-go v0.20.4
	k8s.io/utils v0.0.0-20210111153108-fddb29f9d009
	sigs.k8s.io/controller-runtime v0.8.3
	sigs.k8s.io/kustomize/api v0.8.5
	sigs.k8s.io/kustomize/kyaml v0.10.15
)
