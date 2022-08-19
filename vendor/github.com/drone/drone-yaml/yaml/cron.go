// Copyright 2019 Drone IO, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package yaml

import "errors"

type (
	// Cron is a resource that defines a cron job, used
	// to execute pipelines at scheduled intervals.
	Cron struct {
		Version string `json:"version,omitempty"`
		Kind    string `json:"kind,omitempty"`
		Type    string `json:"type,omitempty"`
		Name    string `json:"name,omitempty"`

		Spec CronSpec `json:"spec,omitempty"`
	}

	// CronSpec defines the cron job.
	CronSpec struct {
		Schedule string         `json:"schedule,omitempty"`
		Branch   string         `json:"branch,omitempty"`
		Deploy   CronDeployment `json:"deployment,omitempty" yaml:"deployment"`
	}

	// CronDeployment defines a cron job deployment.
	CronDeployment struct {
		Target string `json:"target,omitempty"`
	}
)

// GetVersion returns the resource version.
func (c *Cron) GetVersion() string { return c.Version }

// GetKind returns the resource kind.
func (c *Cron) GetKind() string { return c.Kind }

// Validate returns an error if the cron is invalid.
func (c Cron) Validate() error {
	switch {
	case c.Spec.Branch == "":
		return errors.New("yaml: invalid cron branch")
	default:
		return nil
	}
}
