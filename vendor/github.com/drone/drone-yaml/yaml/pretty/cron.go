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

package pretty

import "github.com/drone/drone-yaml/yaml"

// helper function pretty prints the cron resource.
func printCron(w writer, v *yaml.Cron) {
	w.WriteString("---")
	w.WriteTagValue("version", v.Version)
	w.WriteTagValue("kind", v.Kind)
	w.WriteTagValue("name", v.Name)
	printSpec(w, v)
	w.WriteByte('\n')
	w.WriteByte('\n')
}

// helper function pretty prints the spec block.
func printSpec(w writer, v *yaml.Cron) {
	w.WriteTag("spec")

	w.IndentIncrease()
	w.WriteTagValue("schedule", v.Spec.Schedule)
	w.WriteTagValue("branch", v.Spec.Branch)
	if hasDeployment(v) {
		printDeploy(w, v)
	}
	w.IndentDecrease()
}

// helper function pretty prints the deploy block.
func printDeploy(w writer, v *yaml.Cron) {
	w.WriteTag("deployment")
	w.IndentIncrease()
	w.WriteTagValue("target", v.Spec.Deploy.Target)
	w.IndentDecrease()
}

// helper function returns true if the deployment
// object is empty.
func hasDeployment(v *yaml.Cron) bool {
	return v.Spec.Deploy.Target != ""
}
