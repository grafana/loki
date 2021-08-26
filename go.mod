module github.com/ViaQ/loki-operator

go 1.16

require (
	github.com/ViaQ/logerr v1.0.10
	github.com/go-logr/logr v0.4.0
	github.com/google/uuid v1.1.2
	github.com/imdario/mergo v0.3.12
	github.com/maxbrunsfeld/counterfeiter/v6 v6.3.0
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.48.0
	github.com/prometheus/client_golang v1.11.0
	github.com/stretchr/testify v1.7.0
	k8s.io/api v0.21.4
	k8s.io/apimachinery v0.21.4
	k8s.io/client-go v0.21.4
	k8s.io/utils v0.0.0-20210527160623-6fdb442a123b
	sigs.k8s.io/controller-runtime v0.9.2
	sigs.k8s.io/yaml v1.2.0
)
