/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dynamic

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
)

type dynamicClient struct {
	client *rest.RESTClient
}

var _ Interface = &dynamicClient{}

// ConfigFor returns a copy of the provided config with the
// appropriate dynamic client defaults set.
func ConfigFor(inConfig *rest.Config) *rest.Config {
	config := rest.CopyConfig(inConfig)
	config.AcceptContentTypes = "application/json"
	config.ContentType = "application/json"
	config.NegotiatedSerializer = basicNegotiatedSerializer{} // this gets used for discovery and error handling types
	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	return config
}

// NewForConfigOrDie creates a new Interface for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) Interface {
	ret, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return ret
}

// NewForConfig creates a new dynamic client or returns an error.
func NewForConfig(inConfig *rest.Config) (Interface, error) {
	config := ConfigFor(inConfig)
	// for serializing the options
	config.GroupVersion = &schema.GroupVersion{}
	config.APIPath = "/if-you-see-this-search-for-the-break"

	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}

	return &dynamicClient{client: restClient}, nil
}

type dynamicResourceClient struct {
	client    *dynamicClient
	namespace string
	resource  schema.GroupVersionResource
}

func (c *dynamicClient) Resource(resource schema.GroupVersionResource) NamespaceableResourceInterface {
	return &dynamicResourceClient{client: c, resource: resource}
}

func (c *dynamicResourceClient) Namespace(ns string) ResourceInterface {
	ret := *c
	ret.namespace = ns
	return &ret
}

func (c *dynamicResourceClient) Create(ctx context.Context, obj *unstructured.Unstructured, opts metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	outBytes, err := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)
	if err != nil {
		return nil, err
	}
	name := ""
	if len(subresources) > 0 {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return nil, err
		}
		name = accessor.GetName()
		if len(name) == 0 {
			return nil, fmt.Errorf("name is required")
		}
	}

	result := c.client.client.
		Post().
		AbsPath(append(c.makeURLSegments(name), subresources...)...).
		Body(outBytes).
		SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).
		Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}

	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	return uncastObj.(*unstructured.Unstructured), nil
}

func (c *dynamicResourceClient) Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	name := accessor.GetName()
	if len(name) == 0 {
		return nil, fmt.Errorf("name is required")
	}
	outBytes, err := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)
	if err != nil {
		return nil, err
	}

	result := c.client.client.
		Put().
		AbsPath(append(c.makeURLSegments(name), subresources...)...).
		Body(outBytes).
		SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).
		Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}

	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	return uncastObj.(*unstructured.Unstructured), nil
}

func (c *dynamicResourceClient) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	name := accessor.GetName()
	if len(name) == 0 {
		return nil, fmt.Errorf("name is required")
	}

	outBytes, err := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)
	if err != nil {
		return nil, err
	}

	result := c.client.client.
		Put().
		AbsPath(append(c.makeURLSegments(name), "status")...).
		Body(outBytes).
		SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).
		Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}

	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	return uncastObj.(*unstructured.Unstructured), nil
}

func (c *dynamicResourceClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions, subresources ...string) error {
	if len(name) == 0 {
		return fmt.Errorf("name is required")
	}
	deleteOptionsByte, err := runtime.Encode(deleteOptionsCodec.LegacyCodec(schema.GroupVersion{Version: "v1"}), &opts)
	if err != nil {
		return err
	}

	result := c.client.client.
		Delete().
		AbsPath(append(c.makeURLSegments(name), subresources...)...).
		Body(deleteOptionsByte).
		Do(ctx)
	return result.Error()
}

func (c *dynamicResourceClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	deleteOptionsByte, err := runtime.Encode(deleteOptionsCodec.LegacyCodec(schema.GroupVersion{Version: "v1"}), &opts)
	if err != nil {
		return err
	}

	result := c.client.client.
		Delete().
		AbsPath(c.makeURLSegments("")...).
		Body(deleteOptionsByte).
		SpecificallyVersionedParams(&listOptions, dynamicParameterCodec, versionV1).
		Do(ctx)
	return result.Error()
}

func (c *dynamicResourceClient) Get(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if len(name) == 0 {
		return nil, fmt.Errorf("name is required")
	}
	result := c.client.client.Get().AbsPath(append(c.makeURLSegments(name), subresources...)...).SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}
	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	return uncastObj.(*unstructured.Unstructured), nil
}

func (c *dynamicResourceClient) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	result := c.client.client.Get().AbsPath(c.makeURLSegments("")...).SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}
	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	if list, ok := uncastObj.(*unstructured.UnstructuredList); ok {
		return list, nil
	}

	list, err := uncastObj.(*unstructured.Unstructured).ToList()
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (c *dynamicResourceClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.client.Get().AbsPath(c.makeURLSegments("")...).
		SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).
		Watch(ctx)
}

func (c *dynamicResourceClient) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if len(name) == 0 {
		return nil, fmt.Errorf("name is required")
	}
	result := c.client.client.
		Patch(pt).
		AbsPath(append(c.makeURLSegments(name), subresources...)...).
		Body(data).
		SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).
		Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}
	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	return uncastObj.(*unstructured.Unstructured), nil
}

func (c *dynamicResourceClient) makeURLSegments(name string) []string {
	url := []string{}
	if len(c.resource.Group) == 0 {
		url = append(url, "api")
	} else {
		url = append(url, "apis", c.resource.Group)
	}
	url = append(url, c.resource.Version)

	if len(c.namespace) > 0 {
		url = append(url, "namespaces", c.namespace)
	}
	url = append(url, c.resource.Resource)

	if len(name) > 0 {
		url = append(url, name)
	}

	return url
}
