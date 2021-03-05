/*
Copyright 2020 The Kubernetes Authors.

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

package printer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	"github.com/onsi/ginkgo/reporters"
	"github.com/onsi/ginkgo/types"

	"k8s.io/apimachinery/pkg/util/sets"
)

var (
	allRegisteredSuites     = sets.String{}
	allRegisteredSuitesLock = &sync.Mutex{}
)

type prowReporter struct {
	junitReporter *reporters.JUnitReporter
}

// NewProwReporter returns a prowReporter that will write out junit if running in Prow and do
// nothing otherwise.
// WARNING: It seems this does not always properly fail the test runs when there are failures,
// see https://github.com/onsi/ginkgo/issues/706
// When using this you must make sure to grep for failures in your junit xmls and fail the run
// if there are any.
func NewProwReporter(suiteName string) ginkgo.Reporter {
	allRegisteredSuitesLock.Lock()
	if allRegisteredSuites.Has(suiteName) {
		panic(fmt.Sprintf("Suite named %q registered more than once", suiteName))
	}
	allRegisteredSuites.Insert(suiteName)
	allRegisteredSuitesLock.Unlock()

	if os.Getenv("CI") == "" {
		return &prowReporter{}
	}
	artifactsDir := os.Getenv("ARTIFACTS")
	if artifactsDir == "" {
		return &prowReporter{}
	}

	path := filepath.Join(artifactsDir, fmt.Sprintf("junit_%s_%d.xml", suiteName, config.GinkgoConfig.ParallelNode))
	return &prowReporter{
		junitReporter: reporters.NewJUnitReporter(path),
	}
}

func (pr *prowReporter) SpecSuiteWillBegin(config config.GinkgoConfigType, summary *types.SuiteSummary) {
	if pr.junitReporter != nil {
		pr.junitReporter.SpecSuiteWillBegin(config, summary)
	}
}

// BeforeSuiteDidRun implements ginkgo.Reporter
func (pr *prowReporter) BeforeSuiteDidRun(setupSummary *types.SetupSummary) {
	if pr.junitReporter != nil {
		pr.junitReporter.BeforeSuiteDidRun(setupSummary)
	}
}

// AfterSuiteDidRun implements ginkgo.Reporter
func (pr *prowReporter) AfterSuiteDidRun(setupSummary *types.SetupSummary) {
	if pr.junitReporter != nil {
		pr.junitReporter.AfterSuiteDidRun(setupSummary)
	}
}

// SpecWillRun implements ginkgo.Reporter
func (pr *prowReporter) SpecWillRun(specSummary *types.SpecSummary) {
	if pr.junitReporter != nil {
		pr.junitReporter.SpecWillRun(specSummary)
	}
}

// SpecDidComplete implements ginkgo.Reporter
func (pr *prowReporter) SpecDidComplete(specSummary *types.SpecSummary) {
	if pr.junitReporter != nil {
		pr.junitReporter.SpecDidComplete(specSummary)
	}
}

// SpecSuiteDidEnd Prints a newline between "35 Passed | 0 Failed | 0 Pending | 0 Skipped" and "--- PASS:"
func (pr *prowReporter) SpecSuiteDidEnd(summary *types.SuiteSummary) {
	if pr.junitReporter != nil {
		pr.junitReporter.SpecSuiteDidEnd(summary)
	}
}
