package client

import (
	"context"

	loggingv1 "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

type ClusterLogForwarderInterface interface {
	Create(ctx context.Context, clf *loggingv1.ClusterLogForwarder, opts metav1.CreateOptions) (*loggingv1.ClusterLogForwarder, error)
	Update(ctx context.Context, clf *loggingv1.ClusterLogForwarder, opts metav1.UpdateOptions) (*loggingv1.ClusterLogForwarder, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*loggingv1.ClusterLogForwarder, error)
}

type clusterlogforwarders struct {
	client rest.Interface
	ns     string
}

func (c *ClusterLogForwarderV1Client) ClusterLogForwarders(namespace string) ClusterLogForwarderInterface {
	return &clusterlogforwarders{
		client: c.restClient,
		ns:     namespace,
	}
}

type ClusterLogForwarderV1Client struct {
	restClient rest.Interface
}

func (c *clusterlogforwarders) Get(ctx context.Context, name string, opts metav1.GetOptions) (result *loggingv1.ClusterLogForwarder, err error) {
	result = &loggingv1.ClusterLogForwarder{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("clusterlogforwarders").
		Name(name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

func (c *clusterlogforwarders) Create(ctx context.Context, clf *loggingv1.ClusterLogForwarder, opts metav1.CreateOptions) (result *loggingv1.ClusterLogForwarder, err error) {
	result = &loggingv1.ClusterLogForwarder{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("clusterlogforwarders").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(clf).
		Do(ctx).
		Into(result)
	return
}

func (c *clusterlogforwarders) Update(ctx context.Context, clf *loggingv1.ClusterLogForwarder, opts metav1.UpdateOptions) (result *loggingv1.ClusterLogForwarder, err error) {
	result = &loggingv1.ClusterLogForwarder{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("clusterlogforwarders").
		Name(clf.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(clf).
		Do(ctx).
		Into(result)
	return
}

func (c *clusterlogforwarders) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) (err error) {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("clusterlogforwarders").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}
