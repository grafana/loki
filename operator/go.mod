module github.com/grafana/loki/operator

go 1.22.0

toolchain go1.22.5

require (
	github.com/ViaQ/logerr/v2 v2.1.0
	github.com/go-logr/logr v1.4.2
	github.com/google/go-cmp v0.6.0
	github.com/google/uuid v1.6.0
	github.com/grafana/loki/operator/api/loki v0.0.0-00010101000000-000000000000
	github.com/imdario/mergo v0.3.16
	github.com/maxbrunsfeld/counterfeiter/v6 v6.9.0
	github.com/openshift/api v0.0.0-20240912201240-0a8800162826 // release-4.17
	github.com/openshift/cloud-credential-operator v0.0.0-20240908131729-d5b0b95c40f7 // release-4.17
	github.com/openshift/library-go v0.0.0-20240731134552-8211143dfde7 // release-4.17
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.76.1
	github.com/prometheus/client_golang v1.20.5
	github.com/prometheus/common v0.60.0
	github.com/prometheus/prometheus v0.42.0
	github.com/stretchr/testify v1.9.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.30.5
	k8s.io/apimachinery v0.30.5
	k8s.io/apiserver v0.30.5
	k8s.io/client-go v0.30.5
	k8s.io/component-base v0.30.5
	k8s.io/utils v0.0.0-20240921022957-49e7df575cb6
	sigs.k8s.io/controller-runtime v0.18.5
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/evanphx/json-patch/v5 v5.9.0 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/go-openapi/jsonpointer v0.20.0 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.22.4 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/grafana/regexp v0.0.0-20221122212121-6b5c0a4cb7fd // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mwitkow/go-conntrack v0.0.0-20190716064945-2f068394615f // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/exp v0.0.0-20240719175910-8a7402abbf56 // indirect
	golang.org/x/mod v0.21.0 // indirect
	golang.org/x/net v0.29.0 // indirect
	golang.org/x/oauth2 v0.23.0 // indirect
	golang.org/x/sync v0.8.0 // indirect
	golang.org/x/sys v0.25.0 // indirect
	golang.org/x/term v0.24.0 // indirect
	golang.org/x/text v0.18.0 // indirect
	golang.org/x/time v0.5.0 // indirect
	golang.org/x/tools v0.25.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.30.3 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20240228011516-70dd3763d340 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
)

replace github.com/grafana/loki/operator/api/loki => ./api/loki

// Replace v2.4.0+incompatible indirect refs with v5.1.1 for compatibility with google.golang.org/grpc >=v1.56.3
replace github.com/sercand/kuberesolver => github.com/sercand/kuberesolver/v5 v5.1.1
