package client

import (
	"github.com/openshift/cluster-logging-operator/apis"
	loggingv1 "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

func NewClient(cfg *rest.Config) (*ClusterLogForwarderV1Client, error) {
	if err := apis.AddToScheme(scheme.Scheme); err != nil {
		return nil, err
	}
	config := *cfg
	config.GroupVersion = &loggingv1.GroupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)
	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return &ClusterLogForwarderV1Client{restClient: client}, nil
}
