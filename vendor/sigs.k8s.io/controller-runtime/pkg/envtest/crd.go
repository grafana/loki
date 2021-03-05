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

package envtest

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// CRDInstallOptions are the options for installing CRDs
type CRDInstallOptions struct {
	// Paths is a list of paths to the directories or files containing CRDs
	Paths []string

	// CRDs is a list of CRDs to install
	CRDs []client.Object

	// ErrorIfPathMissing will cause an error if a Path does not exist
	ErrorIfPathMissing bool

	// MaxTime is the max time to wait
	MaxTime time.Duration

	// PollInterval is the interval to check
	PollInterval time.Duration

	// CleanUpAfterUse will cause the CRDs listed for installation to be
	// uninstalled when terminating the test environment.
	// Defaults to false.
	CleanUpAfterUse bool
}

const defaultPollInterval = 100 * time.Millisecond
const defaultMaxWait = 10 * time.Second

// InstallCRDs installs a collection of CRDs into a cluster by reading the crd yaml files from a directory
func InstallCRDs(config *rest.Config, options CRDInstallOptions) ([]client.Object, error) {
	defaultCRDOptions(&options)

	// Read the CRD yamls into options.CRDs
	if err := readCRDFiles(&options); err != nil {
		return nil, err
	}

	// Create the CRDs in the apiserver
	if err := CreateCRDs(config, options.CRDs); err != nil {
		return options.CRDs, err
	}

	// Wait for the CRDs to appear as Resources in the apiserver
	if err := WaitForCRDs(config, options.CRDs, options); err != nil {
		return options.CRDs, err
	}

	return options.CRDs, nil
}

// readCRDFiles reads the directories of CRDs in options.Paths and adds the CRD structs to options.CRDs
func readCRDFiles(options *CRDInstallOptions) error {
	if len(options.Paths) > 0 {
		crdList, err := renderCRDs(options)
		if err != nil {
			return err
		}

		options.CRDs = append(options.CRDs, crdList...)
	}
	return nil
}

// defaultCRDOptions sets the default values for CRDs
func defaultCRDOptions(o *CRDInstallOptions) {
	if o.MaxTime == 0 {
		o.MaxTime = defaultMaxWait
	}
	if o.PollInterval == 0 {
		o.PollInterval = defaultPollInterval
	}
}

// WaitForCRDs waits for the CRDs to appear in discovery
func WaitForCRDs(config *rest.Config, crds []client.Object, options CRDInstallOptions) error {
	// Add each CRD to a map of GroupVersion to Resource
	waitingFor := map[schema.GroupVersion]*sets.String{}
	for _, crd := range runtimeCRDListToUnstructured(crds) {
		gvs := []schema.GroupVersion{}
		crdGroup, _, err := unstructured.NestedString(crd.Object, "spec", "group")
		if err != nil {
			return err
		}
		crdPlural, _, err := unstructured.NestedString(crd.Object, "spec", "names", "plural")
		if err != nil {
			return err
		}
		crdVersion, _, err := unstructured.NestedString(crd.Object, "spec", "version")
		if err != nil {
			return err
		}
		versions, found, err := unstructured.NestedSlice(crd.Object, "spec", "versions")
		if err != nil {
			return err
		}

		// gvs should be added here only if single version is found. If multiple version is found we will add those version
		// based on the version is served or not.
		if crdVersion != "" && !found {
			gvs = append(gvs, schema.GroupVersion{Group: crdGroup, Version: crdVersion})
		}

		for _, version := range versions {
			versionMap, ok := version.(map[string]interface{})
			if !ok {
				continue
			}
			served, _, err := unstructured.NestedBool(versionMap, "served")
			if err != nil {
				return err
			}
			if served {
				versionName, _, err := unstructured.NestedString(versionMap, "name")
				if err != nil {
					return err
				}
				gvs = append(gvs, schema.GroupVersion{Group: crdGroup, Version: versionName})
			}
		}

		for _, gv := range gvs {
			log.V(1).Info("adding API in waitlist", "GV", gv)
			if _, found := waitingFor[gv]; !found {
				// Initialize the set
				waitingFor[gv] = &sets.String{}
			}
			// Add the Resource
			waitingFor[gv].Insert(crdPlural)
		}
	}

	// Poll until all resources are found in discovery
	p := &poller{config: config, waitingFor: waitingFor}
	return wait.PollImmediate(options.PollInterval, options.MaxTime, p.poll)
}

// poller checks if all the resources have been found in discovery, and returns false if not
type poller struct {
	// config is used to get discovery
	config *rest.Config

	// waitingFor is the map of resources keyed by group version that have not yet been found in discovery
	waitingFor map[schema.GroupVersion]*sets.String
}

// poll checks if all the resources have been found in discovery, and returns false if not
func (p *poller) poll() (done bool, err error) {
	// Create a new clientset to avoid any client caching of discovery
	cs, err := clientset.NewForConfig(p.config)
	if err != nil {
		return false, err
	}

	allFound := true
	for gv, resources := range p.waitingFor {
		// All resources found, do nothing
		if resources.Len() == 0 {
			delete(p.waitingFor, gv)
			continue
		}

		// Get the Resources for this GroupVersion
		// TODO: Maybe the controller-runtime client should be able to do this...
		resourceList, err := cs.Discovery().ServerResourcesForGroupVersion(gv.Group + "/" + gv.Version)
		if err != nil {
			return false, nil
		}

		// Remove each found resource from the resources set that we are waiting for
		for _, resource := range resourceList.APIResources {
			resources.Delete(resource.Name)
		}

		// Still waiting on some resources in this group version
		if resources.Len() != 0 {
			allFound = false
		}
	}
	return allFound, nil
}

// UninstallCRDs uninstalls a collection of CRDs by reading the crd yaml files from a directory
func UninstallCRDs(config *rest.Config, options CRDInstallOptions) error {

	// Read the CRD yamls into options.CRDs
	if err := readCRDFiles(&options); err != nil {
		return err
	}

	// Delete the CRDs from the apiserver
	cs, err := client.New(config, client.Options{})
	if err != nil {
		return err
	}

	// Uninstall each CRD
	for _, crd := range runtimeCRDListToUnstructured(options.CRDs) {
		log.V(1).Info("uninstalling CRD", "crd", crd.GetName())
		if err := cs.Delete(context.TODO(), crd); err != nil {
			// If CRD is not found, we can consider success
			if !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	return nil
}

// CreateCRDs creates the CRDs
func CreateCRDs(config *rest.Config, crds []client.Object) error {
	cs, err := client.New(config, client.Options{})
	if err != nil {
		return err
	}

	// Create each CRD
	for _, crd := range runtimeCRDListToUnstructured(crds) {
		log.V(1).Info("installing CRD", "crd", crd.GetName())
		existingCrd := crd.DeepCopy()
		err := cs.Get(context.TODO(), client.ObjectKey{Name: crd.GetName()}, existingCrd)
		switch {
		case apierrors.IsNotFound(err):
			if err := cs.Create(context.TODO(), crd); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			log.V(1).Info("CRD already exists, updating", "crd", crd.GetName())
			crd.SetResourceVersion(existingCrd.GetResourceVersion())
			if err := cs.Update(context.TODO(), crd); err != nil {
				return err
			}
		}
	}
	return nil
}

// renderCRDs iterate through options.Paths and extract all CRD files.
func renderCRDs(options *CRDInstallOptions) ([]client.Object, error) {
	var (
		err   error
		info  os.FileInfo
		files []os.FileInfo
	)

	type GVKN struct {
		GVK  schema.GroupVersionKind
		Name string
	}

	crds := map[GVKN]*unstructured.Unstructured{}

	for _, path := range options.Paths {
		var filePath = path

		// Return the error if ErrorIfPathMissing exists
		if info, err = os.Stat(path); os.IsNotExist(err) {
			if options.ErrorIfPathMissing {
				return nil, err
			}
			continue
		}

		if !info.IsDir() {
			filePath, files = filepath.Dir(path), []os.FileInfo{info}
		} else {
			if files, err = ioutil.ReadDir(path); err != nil {
				return nil, err
			}
		}

		log.V(1).Info("reading CRDs from path", "path", path)
		crdList, err := readCRDs(filePath, files)
		if err != nil {
			return nil, err
		}

		for i, crd := range crdList {
			gvkn := GVKN{GVK: crd.GroupVersionKind(), Name: crd.GetName()}
			if _, found := crds[gvkn]; found {
				// Currently, we only print a log when there are duplicates. We may want to error out if that makes more sense.
				log.Info("there are more than one CRD definitions with the same <Group, Version, Kind, Name>", "GVKN", gvkn)
			}
			// We always use the CRD definition that we found last.
			crds[gvkn] = crdList[i]
		}
	}

	// Converting map to a list to return
	var res []client.Object
	for _, obj := range crds {
		res = append(res, obj)
	}
	return res, nil
}

// readCRDs reads the CRDs from files and Unmarshals them into structs
func readCRDs(basePath string, files []os.FileInfo) ([]*unstructured.Unstructured, error) {
	var crds []*unstructured.Unstructured

	// White list the file extensions that may contain CRDs
	crdExts := sets.NewString(".json", ".yaml", ".yml")

	for _, file := range files {
		// Only parse allowlisted file types
		if !crdExts.Has(filepath.Ext(file.Name())) {
			continue
		}

		// Unmarshal CRDs from file into structs
		docs, err := readDocuments(filepath.Join(basePath, file.Name()))
		if err != nil {
			return nil, err
		}

		for _, doc := range docs {
			crd := &unstructured.Unstructured{}
			if err = yaml.Unmarshal(doc, crd); err != nil {
				return nil, err
			}

			// Check that it is actually a CRD
			crdKind, _, err := unstructured.NestedString(crd.Object, "spec", "names", "kind")
			if err != nil {
				return nil, err
			}
			crdGroup, _, err := unstructured.NestedString(crd.Object, "spec", "group")
			if err != nil {
				return nil, err
			}

			if crd.GetKind() != "CustomResourceDefinition" || crdKind == "" || crdGroup == "" {
				continue
			}
			crds = append(crds, crd)
		}

		log.V(1).Info("read CRDs from file", "file", file.Name())
	}
	return crds, nil
}

// readDocuments reads documents from file
func readDocuments(fp string) ([][]byte, error) {
	b, err := ioutil.ReadFile(fp)
	if err != nil {
		return nil, err
	}

	docs := [][]byte{}
	reader := k8syaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(b)))
	for {
		// Read document
		doc, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, err
		}

		docs = append(docs, doc)
	}

	return docs, nil
}
