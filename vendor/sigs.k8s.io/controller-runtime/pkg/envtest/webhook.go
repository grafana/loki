/*
Copyright 2019 The Kubernetes Authors.
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

package envtest

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/internal/testing/integration"
	"sigs.k8s.io/controller-runtime/pkg/internal/testing/integration/addr"
	"sigs.k8s.io/yaml"
)

// WebhookInstallOptions are the options for installing mutating or validating webhooks
type WebhookInstallOptions struct {
	// Paths is a list of paths to the directories or files containing the mutating or validating webhooks yaml or json configs.
	Paths []string

	// MutatingWebhooks is a list of MutatingWebhookConfigurations to install
	MutatingWebhooks []client.Object

	// ValidatingWebhooks is a list of ValidatingWebhookConfigurations to install
	ValidatingWebhooks []client.Object

	// IgnoreErrorIfPathMissing will ignore an error if a DirectoryPath does not exist when set to true
	IgnoreErrorIfPathMissing bool

	// LocalServingHost is the host for serving webhooks on.
	// it will be automatically populated
	LocalServingHost string

	// LocalServingPort is the allocated port for serving webhooks on.
	// it will be automatically populated by a random available local port
	LocalServingPort int

	// LocalServingCertDir is the allocated directory for serving certificates.
	// it will be automatically populated by the local temp dir
	LocalServingCertDir string

	// CAData is the CA that can be used to trust the serving certificates in LocalServingCertDir.
	LocalServingCAData []byte

	// LocalServingHostExternalName is the hostname to use to reach the webhook server.
	LocalServingHostExternalName string

	// MaxTime is the max time to wait
	MaxTime time.Duration

	// PollInterval is the interval to check
	PollInterval time.Duration
}

// ModifyWebhookDefinitions modifies webhook definitions by:
// - applying CABundle based on the provided tinyca
// - if webhook client config uses service spec, it's removed and replaced with direct url
func (o *WebhookInstallOptions) ModifyWebhookDefinitions(caData []byte) error {
	hostPort, err := o.generateHostPort()
	if err != nil {
		return err
	}

	for i, unstructuredHook := range runtimeListToUnstructured(o.MutatingWebhooks) {
		webhooks, found, err := unstructured.NestedSlice(unstructuredHook.Object, "webhooks")
		if !found || err != nil {
			return fmt.Errorf("unexpected object, %v", err)
		}
		for j := range webhooks {
			webhook, err := modifyWebhook(webhooks[j].(map[string]interface{}), caData, hostPort)
			if err != nil {
				return err
			}
			webhooks[j] = webhook
			unstructuredHook.Object["webhooks"] = webhooks
			o.MutatingWebhooks[i] = unstructuredHook
		}
	}

	for i, unstructuredHook := range runtimeListToUnstructured(o.ValidatingWebhooks) {
		webhooks, found, err := unstructured.NestedSlice(unstructuredHook.Object, "webhooks")
		if !found || err != nil {
			return fmt.Errorf("unexpected object, %v", err)
		}
		for j := range webhooks {
			webhook, err := modifyWebhook(webhooks[j].(map[string]interface{}), caData, hostPort)
			if err != nil {
				return err
			}
			webhooks[j] = webhook
			unstructuredHook.Object["webhooks"] = webhooks
			o.ValidatingWebhooks[i] = unstructuredHook
		}
	}
	return nil
}

func modifyWebhook(webhook map[string]interface{}, caData []byte, hostPort string) (map[string]interface{}, error) {
	clientConfig, found, err := unstructured.NestedMap(webhook, "clientConfig")
	if !found || err != nil {
		return nil, fmt.Errorf("cannot find clientconfig: %v", err)
	}
	clientConfig["caBundle"] = base64.StdEncoding.EncodeToString(caData)
	servicePath, found, err := unstructured.NestedString(clientConfig, "service", "path")
	if found && err == nil {
		// we cannot use service in integration tests since we're running controller outside cluster
		// the intent here is that we swap out service for raw address because we don't have an actually standard kube service network.
		// We want to users to be able to use your standard config though
		url := fmt.Sprintf("https://%s/%s", hostPort, servicePath)
		clientConfig["url"] = url
		clientConfig["service"] = nil
	}
	webhook["clientConfig"] = clientConfig
	return webhook, nil
}

func (o *WebhookInstallOptions) generateHostPort() (string, error) {
	if o.LocalServingPort == 0 {
		port, host, err := addr.Suggest(o.LocalServingHost)
		if err != nil {
			return "", fmt.Errorf("unable to grab random port for serving webhooks on: %v", err)
		}
		o.LocalServingPort = port
		o.LocalServingHost = host
	}
	host := o.LocalServingHostExternalName
	if host == "" {
		host = o.LocalServingHost
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", o.LocalServingPort)), nil
}

// PrepWithoutInstalling does the setup parts of Install (populating host-port,
// setting up CAs, etc), without actually truing to do anything with webhook
// definitions.  This is largely useful for internal testing of
// controller-runtime, where we need a random host-port & caData for webhook
// tests, but may be useful in similar scenarios.
func (o *WebhookInstallOptions) PrepWithoutInstalling() error {
	hookCA, err := o.setupCA()
	if err != nil {
		return err
	}
	if err := parseWebhook(o); err != nil {
		return err
	}

	err = o.ModifyWebhookDefinitions(hookCA)
	if err != nil {
		return err
	}

	return nil
}

// Install installs specified webhooks to the API server
func (o *WebhookInstallOptions) Install(config *rest.Config) error {
	if err := o.PrepWithoutInstalling(); err != nil {
		return err
	}

	if err := createWebhooks(config, o.MutatingWebhooks, o.ValidatingWebhooks); err != nil {
		return err
	}

	if err := WaitForWebhooks(config, o.MutatingWebhooks, o.ValidatingWebhooks, *o); err != nil {
		return err
	}

	return nil
}

// Cleanup cleans up cert directories
func (o *WebhookInstallOptions) Cleanup() error {
	if o.LocalServingCertDir != "" {
		return os.RemoveAll(o.LocalServingCertDir)
	}
	return nil
}

// WaitForWebhooks waits for the Webhooks to be available through API server
func WaitForWebhooks(config *rest.Config,
	mutatingWebhooks []client.Object,
	validatingWebhooks []client.Object,
	options WebhookInstallOptions) error {

	waitingFor := map[schema.GroupVersionKind]*sets.String{}

	for _, hook := range runtimeListToUnstructured(append(validatingWebhooks, mutatingWebhooks...)) {
		if _, ok := waitingFor[hook.GroupVersionKind()]; !ok {
			waitingFor[hook.GroupVersionKind()] = &sets.String{}
		}
		waitingFor[hook.GroupVersionKind()].Insert(hook.GetName())
	}

	// Poll until all resources are found in discovery
	p := &webhookPoller{config: config, waitingFor: waitingFor}
	return wait.PollImmediate(options.PollInterval, options.MaxTime, p.poll)
}

// poller checks if all the resources have been found in discovery, and returns false if not
type webhookPoller struct {
	// config is used to get discovery
	config *rest.Config

	// waitingFor is the map of resources keyed by group version that have not yet been found in discovery
	waitingFor map[schema.GroupVersionKind]*sets.String
}

// poll checks if all the resources have been found in discovery, and returns false if not
func (p *webhookPoller) poll() (done bool, err error) {
	// Create a new clientset to avoid any client caching of discovery
	c, err := client.New(p.config, client.Options{})
	if err != nil {
		return false, err
	}

	allFound := true
	for gvk, names := range p.waitingFor {
		if names.Len() == 0 {
			delete(p.waitingFor, gvk)
			continue
		}
		for _, name := range names.List() {
			var obj = &unstructured.Unstructured{}
			obj.SetGroupVersionKind(gvk)
			err := c.Get(context.Background(), client.ObjectKey{
				Namespace: "",
				Name:      name,
			}, obj)

			if err == nil {
				names.Delete(name)
			}

			if errors.IsNotFound(err) {
				allFound = false
			}
			if err != nil {
				return false, err
			}
		}
	}
	return allFound, nil
}

// setupCA creates CA for testing and writes them to disk
func (o *WebhookInstallOptions) setupCA() ([]byte, error) {
	hookCA, err := integration.NewTinyCA()
	if err != nil {
		return nil, fmt.Errorf("unable to set up webhook CA: %v", err)
	}

	names := []string{"localhost", o.LocalServingHost, o.LocalServingHostExternalName}
	hookCert, err := hookCA.NewServingCert(names...)
	if err != nil {
		return nil, fmt.Errorf("unable to set up webhook serving certs: %v", err)
	}

	localServingCertsDir, err := ioutil.TempDir("", "envtest-serving-certs-")
	o.LocalServingCertDir = localServingCertsDir
	if err != nil {
		return nil, fmt.Errorf("unable to create directory for webhook serving certs: %v", err)
	}

	certData, keyData, err := hookCert.AsBytes()
	if err != nil {
		return nil, fmt.Errorf("unable to marshal webhook serving certs: %v", err)
	}

	if err := ioutil.WriteFile(filepath.Join(localServingCertsDir, "tls.crt"), certData, 0640); err != nil {
		return nil, fmt.Errorf("unable to write webhook serving cert to disk: %v", err)
	}
	if err := ioutil.WriteFile(filepath.Join(localServingCertsDir, "tls.key"), keyData, 0640); err != nil {
		return nil, fmt.Errorf("unable to write webhook serving key to disk: %v", err)
	}

	o.LocalServingCAData = certData
	return certData, nil
}

func createWebhooks(config *rest.Config, mutHooks []client.Object, valHooks []client.Object) error {
	cs, err := client.New(config, client.Options{})
	if err != nil {
		return err
	}

	// Create each webhook
	for _, hook := range runtimeListToUnstructured(mutHooks) {
		log.V(1).Info("installing mutating webhook", "webhook", hook.GetName())
		if err := ensureCreated(cs, hook); err != nil {
			return err
		}
	}
	for _, hook := range runtimeListToUnstructured(valHooks) {
		log.V(1).Info("installing validating webhook", "webhook", hook.GetName())
		if err := ensureCreated(cs, hook); err != nil {
			return err
		}
	}
	return nil
}

// ensureCreated creates or update object if already exists in the cluster
func ensureCreated(cs client.Client, obj *unstructured.Unstructured) error {
	existing := obj.DeepCopy()
	err := cs.Get(context.Background(), client.ObjectKey{Name: obj.GetName()}, existing)
	switch {
	case apierrors.IsNotFound(err):
		if err := cs.Create(context.Background(), obj); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		log.V(1).Info("Webhook configuration already exists, updating", "webhook", obj.GetName())
		obj.SetResourceVersion(existing.GetResourceVersion())
		if err := cs.Update(context.Background(), obj); err != nil {
			return err
		}
	}
	return nil
}

// parseWebhook reads the directories or files of Webhooks in options.Paths and adds the Webhook structs to options
func parseWebhook(options *WebhookInstallOptions) error {
	if len(options.Paths) > 0 {
		for _, path := range options.Paths {
			_, err := os.Stat(path)
			if options.IgnoreErrorIfPathMissing && os.IsNotExist(err) {
				continue // skip this path
			}
			if !options.IgnoreErrorIfPathMissing && os.IsNotExist(err) {
				return err // treat missing path as error
			}
			mutHooks, valHooks, err := readWebhooks(path)
			if err != nil {
				return err
			}
			options.MutatingWebhooks = append(options.MutatingWebhooks, mutHooks...)
			options.ValidatingWebhooks = append(options.ValidatingWebhooks, valHooks...)
		}
	}
	return nil
}

// readWebhooks reads the Webhooks from files and Unmarshals them into structs
// returns slice of mutating and validating webhook configurations
func readWebhooks(path string) ([]client.Object, []client.Object, error) {
	// Get the webhook files
	var files []os.FileInfo
	var err error
	log.V(1).Info("reading Webhooks from path", "path", path)
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		path, files = filepath.Dir(path), []os.FileInfo{info}
	} else {
		if files, err = ioutil.ReadDir(path); err != nil {
			return nil, nil, err
		}
	}

	// file extensions that may contain Webhooks
	resourceExtensions := sets.NewString(".json", ".yaml", ".yml")

	var mutHooks []client.Object
	var valHooks []client.Object
	for _, file := range files {
		// Only parse allowlisted file types
		if !resourceExtensions.Has(filepath.Ext(file.Name())) {
			continue
		}

		// Unmarshal Webhooks from file into structs
		docs, err := readDocuments(filepath.Join(path, file.Name()))
		if err != nil {
			return nil, nil, err
		}

		for _, doc := range docs {
			var generic metav1.PartialObjectMetadata
			if err = yaml.Unmarshal(doc, &generic); err != nil {
				return nil, nil, err
			}

			const (
				admissionregv1      = "admissionregistration.k8s.io/v1"
				admissionregv1beta1 = "admissionregistration.k8s.io/v1beta1"
			)
			switch {
			case generic.Kind == "MutatingWebhookConfiguration":
				if generic.APIVersion != admissionregv1beta1 && generic.APIVersion != admissionregv1 {
					return nil, nil, fmt.Errorf("only v1beta1 and v1 are supported right now for MutatingWebhookConfiguration (name: %s)", generic.Name)
				}
				hook := &unstructured.Unstructured{}
				if err := yaml.Unmarshal(doc, &hook); err != nil {
					return nil, nil, err
				}
				mutHooks = append(mutHooks, hook)
			case generic.Kind == "ValidatingWebhookConfiguration":
				if generic.APIVersion != admissionregv1beta1 && generic.APIVersion != admissionregv1 {
					return nil, nil, fmt.Errorf("only v1beta1 and v1 are supported right now for ValidatingWebhookConfiguration (name: %s)", generic.Name)
				}
				hook := &unstructured.Unstructured{}
				if err := yaml.Unmarshal(doc, &hook); err != nil {
					return nil, nil, err
				}
				valHooks = append(valHooks, hook)
			default:
				continue
			}
		}

		log.V(1).Info("read webhooks from file", "file", file.Name())
	}
	return mutHooks, valHooks, nil
}

func runtimeListToUnstructured(l []client.Object) []*unstructured.Unstructured {
	res := []*unstructured.Unstructured{}
	for _, obj := range l {
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj.DeepCopyObject())
		if err != nil {
			continue
		}
		res = append(res, &unstructured.Unstructured{
			Object: m,
		})
	}
	return res
}
