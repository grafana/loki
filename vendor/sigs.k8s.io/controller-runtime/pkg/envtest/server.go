/*
Copyright 2016 The Kubernetes Authors.

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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/internal/testing/integration"

	logf "sigs.k8s.io/controller-runtime/pkg/internal/log"
)

var log = logf.RuntimeLog.WithName("test-env")

/*
It's possible to override some defaults, by setting the following environment variables:
	USE_EXISTING_CLUSTER (boolean): if set to true, envtest will use an existing cluster
	TEST_ASSET_KUBE_APISERVER (string): path to the api-server binary to use
	TEST_ASSET_ETCD (string): path to the etcd binary to use
	TEST_ASSET_KUBECTL (string): path to the kubectl binary to use
	KUBEBUILDER_ASSETS (string): directory containing the binaries to use (api-server, etcd and kubectl). Defaults to /usr/local/kubebuilder/bin.
	KUBEBUILDER_CONTROLPLANE_START_TIMEOUT (string supported by time.ParseDuration): timeout for test control plane to start. Defaults to 20s.
	KUBEBUILDER_CONTROLPLANE_STOP_TIMEOUT (string supported by time.ParseDuration): timeout for test control plane to start. Defaults to 20s.
	KUBEBUILDER_ATTACH_CONTROL_PLANE_OUTPUT (boolean): if set to true, the control plane's stdout and stderr are attached to os.Stdout and os.Stderr

*/
const (
	envUseExistingCluster  = "USE_EXISTING_CLUSTER"
	envKubeAPIServerBin    = "TEST_ASSET_KUBE_APISERVER"
	envEtcdBin             = "TEST_ASSET_ETCD"
	envKubectlBin          = "TEST_ASSET_KUBECTL"
	envKubebuilderPath     = "KUBEBUILDER_ASSETS"
	envStartTimeout        = "KUBEBUILDER_CONTROLPLANE_START_TIMEOUT"
	envStopTimeout         = "KUBEBUILDER_CONTROLPLANE_STOP_TIMEOUT"
	envAttachOutput        = "KUBEBUILDER_ATTACH_CONTROL_PLANE_OUTPUT"
	defaultKubebuilderPath = "/usr/local/kubebuilder/bin"
	StartTimeout           = 60
	StopTimeout            = 60

	defaultKubebuilderControlPlaneStartTimeout = 20 * time.Second
	defaultKubebuilderControlPlaneStopTimeout  = 20 * time.Second
)

// getBinAssetPath returns a path for binary from the following list of locations,
// ordered by precedence:
// 0. KUBEBUILDER_ASSETS
// 1. Environment.BinaryAssetsDirectory
// 2. The default path, "/usr/local/kubebuilder/bin"
func (te *Environment) getBinAssetPath(binary string) string {
	valueFromEnvVar := os.Getenv(envKubebuilderPath)
	if valueFromEnvVar != "" {
		return filepath.Join(valueFromEnvVar, binary)
	}

	if te.BinaryAssetsDirectory != "" {
		return filepath.Join(te.BinaryAssetsDirectory, binary)
	}

	return filepath.Join(defaultKubebuilderPath, binary)
}

// ControlPlane is the re-exported ControlPlane type from the internal integration package
type ControlPlane = integration.ControlPlane

// APIServer is the re-exported APIServer type from the internal integration package
type APIServer = integration.APIServer

// Etcd is the re-exported Etcd type from the internal integration package
type Etcd = integration.Etcd

// Environment creates a Kubernetes test environment that will start / stop the Kubernetes control plane and
// install extension APIs
type Environment struct {
	// ControlPlane is the ControlPlane including the apiserver and etcd
	ControlPlane integration.ControlPlane

	// Config can be used to talk to the apiserver.  It's automatically
	// populated if not set using the standard controller-runtime config
	// loading.
	Config *rest.Config

	// CRDInstallOptions are the options for installing CRDs.
	CRDInstallOptions CRDInstallOptions

	// WebhookInstallOptions are the options for installing webhooks.
	WebhookInstallOptions WebhookInstallOptions

	// ErrorIfCRDPathMissing provides an interface for the underlying
	// CRDInstallOptions.ErrorIfPathMissing. It prevents silent failures
	// for missing CRD paths.
	ErrorIfCRDPathMissing bool

	// CRDs is a list of CRDs to install.
	// If both this field and CRDs field in CRDInstallOptions are specified, the
	// values are merged.
	CRDs []client.Object

	// CRDDirectoryPaths is a list of paths containing CRD yaml or json configs.
	// If both this field and Paths field in CRDInstallOptions are specified, the
	// values are merged.
	CRDDirectoryPaths []string

	// BinaryAssetsDirectory is the path where the binaries required for the envtest are
	// located in the local environment. This field can be overridden by setting KUBEBUILDER_ASSETS.
	BinaryAssetsDirectory string

	// UseExisting indicates that this environments should use an
	// existing kubeconfig, instead of trying to stand up a new control plane.
	// This is useful in cases that need aggregated API servers and the like.
	UseExistingCluster *bool

	// ControlPlaneStartTimeout is the maximum duration each controlplane component
	// may take to start. It defaults to the KUBEBUILDER_CONTROLPLANE_START_TIMEOUT
	// environment variable or 20 seconds if unspecified
	ControlPlaneStartTimeout time.Duration

	// ControlPlaneStopTimeout is the maximum duration each controlplane component
	// may take to stop. It defaults to the KUBEBUILDER_CONTROLPLANE_STOP_TIMEOUT
	// environment variable or 20 seconds if unspecified
	ControlPlaneStopTimeout time.Duration

	// KubeAPIServerFlags is the set of flags passed while starting the api server.
	KubeAPIServerFlags []string

	// AttachControlPlaneOutput indicates if control plane output will be attached to os.Stdout and os.Stderr.
	// Enable this to get more visibility of the testing control plane.
	// It respect KUBEBUILDER_ATTACH_CONTROL_PLANE_OUTPUT environment variable.
	AttachControlPlaneOutput bool
}

// Stop stops a running server.
// Previously installed CRDs, as listed in CRDInstallOptions.CRDs, will be uninstalled
// if CRDInstallOptions.CleanUpAfterUse are set to true.
func (te *Environment) Stop() error {
	if te.CRDInstallOptions.CleanUpAfterUse {
		if err := UninstallCRDs(te.Config, te.CRDInstallOptions); err != nil {
			return err
		}
	}
	if te.useExistingCluster() {
		return nil
	}
	err := te.WebhookInstallOptions.Cleanup()
	if err != nil {
		return err
	}
	return te.ControlPlane.Stop()
}

// getAPIServerFlags returns flags to be used with the Kubernetes API server.
// it returns empty slice for api server defined defaults to be applied if no args specified
func (te Environment) getAPIServerFlags() []string {
	// Set default API server flags if not set.
	if len(te.KubeAPIServerFlags) == 0 {
		return []string{}
	}
	// Check KubeAPIServerFlags contains service-cluster-ip-range, if not, set default value to service-cluster-ip-range
	containServiceClusterIPRange := false
	for _, flag := range te.KubeAPIServerFlags {
		if strings.Contains(flag, "service-cluster-ip-range") {
			containServiceClusterIPRange = true
			break
		}
	}
	if !containServiceClusterIPRange {
		te.KubeAPIServerFlags = append(te.KubeAPIServerFlags, "--service-cluster-ip-range=10.0.0.0/24")
	}
	return te.KubeAPIServerFlags
}

// Start starts a local Kubernetes server and updates te.ApiserverPort with the port it is listening on
func (te *Environment) Start() (*rest.Config, error) {
	if te.useExistingCluster() {
		log.V(1).Info("using existing cluster")
		if te.Config == nil {
			// we want to allow people to pass in their own config, so
			// only load a config if it hasn't already been set.
			log.V(1).Info("automatically acquiring client configuration")

			var err error
			te.Config, err = config.GetConfig()
			if err != nil {
				return nil, err
			}
		}
	} else {
		if te.ControlPlane.APIServer == nil {
			te.ControlPlane.APIServer = &integration.APIServer{Args: te.getAPIServerFlags()}
		}
		if te.ControlPlane.Etcd == nil {
			te.ControlPlane.Etcd = &integration.Etcd{}
		}

		if os.Getenv(envAttachOutput) == "true" {
			te.AttachControlPlaneOutput = true
		}
		if te.ControlPlane.APIServer.Out == nil && te.AttachControlPlaneOutput {
			te.ControlPlane.APIServer.Out = os.Stdout
		}
		if te.ControlPlane.APIServer.Err == nil && te.AttachControlPlaneOutput {
			te.ControlPlane.APIServer.Err = os.Stderr
		}
		if te.ControlPlane.Etcd.Out == nil && te.AttachControlPlaneOutput {
			te.ControlPlane.Etcd.Out = os.Stdout
		}
		if te.ControlPlane.Etcd.Err == nil && te.AttachControlPlaneOutput {
			te.ControlPlane.Etcd.Err = os.Stderr
		}

		if os.Getenv(envKubeAPIServerBin) == "" {
			te.ControlPlane.APIServer.Path = te.getBinAssetPath("kube-apiserver")
		}
		if os.Getenv(envEtcdBin) == "" {
			te.ControlPlane.Etcd.Path = te.getBinAssetPath("etcd")
		}
		if os.Getenv(envKubectlBin) == "" {
			// we can't just set the path manually (it's behind a function), so set the environment variable instead
			if err := os.Setenv(envKubectlBin, te.getBinAssetPath("kubectl")); err != nil {
				return nil, err
			}
		}

		if err := te.defaultTimeouts(); err != nil {
			return nil, fmt.Errorf("failed to default controlplane timeouts: %w", err)
		}
		te.ControlPlane.Etcd.StartTimeout = te.ControlPlaneStartTimeout
		te.ControlPlane.Etcd.StopTimeout = te.ControlPlaneStopTimeout
		te.ControlPlane.APIServer.StartTimeout = te.ControlPlaneStartTimeout
		te.ControlPlane.APIServer.StopTimeout = te.ControlPlaneStopTimeout

		log.V(1).Info("starting control plane", "api server flags", te.ControlPlane.APIServer.Args)
		if err := te.startControlPlane(); err != nil {
			return nil, err
		}

		// Create the *rest.Config for creating new clients
		te.Config = &rest.Config{
			Host: te.ControlPlane.APIURL().Host,
			// gotta go fast during tests -- we don't really care about overwhelming our test API server
			QPS:   1000.0,
			Burst: 2000.0,
		}
	}

	log.V(1).Info("installing CRDs")
	te.CRDInstallOptions.CRDs = mergeCRDs(te.CRDInstallOptions.CRDs, te.CRDs)
	te.CRDInstallOptions.Paths = mergePaths(te.CRDInstallOptions.Paths, te.CRDDirectoryPaths)
	te.CRDInstallOptions.ErrorIfPathMissing = te.ErrorIfCRDPathMissing
	crds, err := InstallCRDs(te.Config, te.CRDInstallOptions)
	if err != nil {
		return te.Config, err
	}
	te.CRDs = crds

	log.V(1).Info("installing webhooks")
	err = te.WebhookInstallOptions.Install(te.Config)

	return te.Config, err
}

func (te *Environment) startControlPlane() error {
	numTries, maxRetries := 0, 5
	var err error
	for ; numTries < maxRetries; numTries++ {
		// Start the control plane - retry if it fails
		err = te.ControlPlane.Start()
		if err == nil {
			break
		}
		log.Error(err, "unable to start the controlplane", "tries", numTries)
	}
	if numTries == maxRetries {
		return fmt.Errorf("failed to start the controlplane. retried %d times: %w", numTries, err)
	}
	return nil
}

func (te *Environment) defaultTimeouts() error {
	var err error
	if te.ControlPlaneStartTimeout == 0 {
		if envVal := os.Getenv(envStartTimeout); envVal != "" {
			te.ControlPlaneStartTimeout, err = time.ParseDuration(envVal)
			if err != nil {
				return err
			}
		} else {
			te.ControlPlaneStartTimeout = defaultKubebuilderControlPlaneStartTimeout
		}
	}

	if te.ControlPlaneStopTimeout == 0 {
		if envVal := os.Getenv(envStopTimeout); envVal != "" {
			te.ControlPlaneStopTimeout, err = time.ParseDuration(envVal)
			if err != nil {
				return err
			}
		} else {
			te.ControlPlaneStopTimeout = defaultKubebuilderControlPlaneStopTimeout
		}
	}
	return nil
}

func (te *Environment) useExistingCluster() bool {
	if te.UseExistingCluster == nil {
		return strings.ToLower(os.Getenv(envUseExistingCluster)) == "true"
	}
	return *te.UseExistingCluster
}

// DefaultKubeAPIServerFlags exposes the default args for the APIServer so that
// you can use those to append your own additional arguments.
var DefaultKubeAPIServerFlags = integration.APIServerDefaultArgs
