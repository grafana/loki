module github.com/ViaQ/loki-operator

go 1.16

require (
	github.com/ViaQ/logerr v1.0.10
	github.com/go-logr/logr v0.4.0
	github.com/google/uuid v1.1.2
	github.com/imdario/mergo v0.3.12
	github.com/maxbrunsfeld/counterfeiter/v6 v6.3.0
	github.com/openshift/api v0.0.0-20210901140736-d8ed1449662d // release-4.9
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.48.0
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/common v0.32.0
	github.com/stretchr/testify v1.7.0
	k8s.io/api v0.22.1
	k8s.io/apimachinery v0.22.1
	k8s.io/client-go v0.22.1
	k8s.io/utils v0.0.0-20210707171843-4b05e18ac7d9
	sigs.k8s.io/controller-runtime v0.9.2
	sigs.k8s.io/yaml v1.2.0
)
