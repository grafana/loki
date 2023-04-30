package kubernetes

import (
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/version"
	prometheusKubernetes "github.com/prometheus/prometheus/discovery/kubernetes"
	kubernetesClient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var userAgent = fmt.Sprintf("Promtail/%s", version.Version)

// From vendor/github.com/prometheus/prometheus/discovery/kubernetes/kubernetes.go::New
func newKubernetesClient(l log.Logger, conf *prometheusKubernetes.SDConfig) (kubernetesClient.Interface, error) {
	if l == nil {
		l = log.NewNopLogger()
	}
	var (
		kcfg *rest.Config
		err  error
	)
	switch {
	case conf.KubeConfig != "":
		kcfg, err = clientcmd.BuildConfigFromFlags("", conf.KubeConfig)
		if err != nil {
			return nil, err
		}
	case conf.APIServer.URL == nil:
		// Use the Kubernetes provided pod service account
		// as described in https://kubernetes.io/docs/admin/service-accounts-admin/
		kcfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}

		level.Info(l).Log("msg", "Using pod service account via in-cluster config")
	default:
		rt, err := config.NewRoundTripperFromConfig(conf.HTTPClientConfig, "kubernetes_sd")
		if err != nil {
			return nil, err
		}
		kcfg = &rest.Config{
			Host:      conf.APIServer.String(),
			Transport: rt,
		}
	}

	kcfg.UserAgent = userAgent
	kcfg.ContentType = "application/vnd.kubernetes.protobuf"

	return kubernetesClient.NewForConfig(kcfg)
}
