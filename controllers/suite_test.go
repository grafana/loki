/*
Copyright 2021.

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

package controllers

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ViaQ/logerr/log"
	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	// +kubebuilder:scaffold:imports
)

var (
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestMain(m *testing.M) {
	testing.Init()
	flag.Parse()

	// Do not run this unless we intend to run integration testing. This stuff is dark magic.
	if envStr := os.Getenv("INTEGRATION"); envStr == "" {
		return
	}
	if testing.Short() {
		return
	}

	Setup()
	defer Teardown()
	m.Run()
}

func Setup() {
	log.Init("integration-testing")
	log.Info("bootstrapping integration test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
	}

	var err error
	cfg, err = testEnv.Start()
	if err != nil {
		log.Error(err, "failed to start test env")
		os.Exit(1)
	}
	if cfg == nil {
		log.Error(fmt.Errorf("config was nil"), "failed to start test env")
		os.Exit(1)
	}

	if err = lokiv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Error(err, "failed to AddToScheme")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		log.Error(err, "failed to create test k8sclient")
		os.Exit(1)
	}

	if cfg == nil {
		log.Error(fmt.Errorf("k8sClient was nil"), "failed to connect k8s client")
		os.Exit(1)
	}
}

func Teardown() {
	log.Info("tearing down test suite")
	testEnv.Stop()
}
