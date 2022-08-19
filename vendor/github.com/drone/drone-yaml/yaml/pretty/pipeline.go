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

import (
	"github.com/drone/drone-yaml/yaml"
)

// helper function to pretty print the pipeline resource.
func printPipeline(w writer, v *yaml.Pipeline) {
	w.WriteString("---")
	w.WriteTagValue("version", v.Version)
	w.WriteTagValue("kind", v.Kind)
	w.WriteTagValue("type", v.Type)
	w.WriteTagValue("name", v.Name)
	w.WriteByte('\n')

	if !isPlatformEmpty(v.Platform) {
		printPlatform(w, v.Platform)
	} else {
		printPlatformDefault(w)
	}
	if !isCloneEmpty(v.Clone) {
		printClone(w, v.Clone)
	}
	if !isConcurrencyEmpty(v.Concurrency) {
		printConcurrency(w, v.Concurrency)
	}
	if !isWorkspaceEmpty(v.Workspace) {
		printWorkspace(w, v.Workspace)
	}

	if len(v.Steps) > 0 {
		w.WriteTag("steps")
		for _, step := range v.Steps {
			if step == nil {
				continue
			}
			seq := new(indexWriter)
			seq.writer = w
			seq.IndentIncrease()
			printContainer(seq, step)
			seq.IndentDecrease()
		}
	}

	if len(v.Services) > 0 {
		w.WriteTag("services")
		for _, step := range v.Services {
			if step == nil {
				continue
			}
			seq := new(indexWriter)
			seq.writer = w
			seq.IndentIncrease()
			printContainer(seq, step)
			seq.IndentDecrease()
		}
	}

	if len(v.Volumes) != 0 {
		printVolumes(w, v.Volumes)
		w.WriteByte('\n')
	}

	if len(v.PullSecrets) > 0 {
		w.WriteTagValue("image_pull_secrets", v.PullSecrets)
		w.WriteByte('\n')
	}

	if len(v.Node) > 0 {
		printNode(w, v.Node)
		w.WriteByte('\n')
	}

	if !isConditionsEmpty(v.Trigger) {
		printConditions(w, "trigger", v.Trigger)
		w.WriteByte('\n')
	}

	if len(v.DependsOn) > 0 {
		printDependsOn(w, v.DependsOn)
		w.WriteByte('\n')
	}

	w.WriteByte('\n')
}

// helper function pretty prints the clone block.
func printClone(w writer, v yaml.Clone) {
	w.WriteTag("clone")
	w.IndentIncrease()
	w.WriteTagValue("depth", v.Depth)
	w.WriteTagValue("disable", v.Disable)
	w.WriteTagValue("skip_verify", v.SkipVerify)
	w.WriteByte('\n')
	w.IndentDecrease()
}

// helper function pretty prints the clone block.
func printConcurrency(w writer, v yaml.Concurrency) {
	w.WriteTag("concurrency")
	w.IndentIncrease()
	w.WriteTagValue("limit", v.Limit)
	w.WriteByte('\n')
	w.IndentDecrease()
}

// helper function pretty prints the conditions mapping.
func printConditions(w writer, name string, v yaml.Conditions) {
	w.WriteTag(name)
	w.IndentIncrease()
	if !isConditionEmpty(v.Action) {
		printCondition(w, "action", v.Action)
	}
	if !isConditionEmpty(v.Branch) {
		printCondition(w, "branch", v.Branch)
	}
	if !isConditionEmpty(v.Cron) {
		printCondition(w, "cron", v.Cron)
	}
	if !isConditionEmpty(v.Event) {
		printCondition(w, "event", v.Event)
	}
	if !isConditionEmpty(v.Instance) {
		printCondition(w, "instance", v.Instance)
	}
	if !isConditionEmpty(v.Paths) {
		printCondition(w, "paths", v.Paths)
	}
	if !isConditionEmpty(v.Ref) {
		printCondition(w, "ref", v.Ref)
	}
	if !isConditionEmpty(v.Repo) {
		printCondition(w, "repo", v.Repo)
	}
	if !isConditionEmpty(v.Status) {
		printCondition(w, "status", v.Status)
	}
	if !isConditionEmpty(v.Target) {
		printCondition(w, "target", v.Target)
	}
	w.IndentDecrease()
}

// helper function pretty prints a condition mapping.
func printCondition(w writer, k string, v yaml.Condition) {
	w.WriteTag(k)
	if len(v.Include) != 0 && len(v.Exclude) == 0 {
		w.WriteByte('\n')
		w.Indent()
		writeValue(w, v.Include)
	}
	if len(v.Include) != 0 && len(v.Exclude) != 0 {
		w.IndentIncrease()
		w.WriteTagValue("include", v.Include)
		w.IndentDecrease()
	}
	if len(v.Exclude) != 0 {
		w.IndentIncrease()
		w.WriteTagValue("exclude", v.Exclude)
		w.IndentDecrease()
	}
}

// helper function pretty prints the node mapping.
func printNode(w writer, v map[string]string) {
	w.WriteTagValue("node", v)
}

// helper function pretty prints the target platform.
func printPlatform(w writer, v yaml.Platform) {
	w.WriteTag("platform")
	w.IndentIncrease()
	w.WriteTagValue("os", v.OS)
	w.WriteTagValue("arch", v.Arch)
	w.WriteTagValue("variant", v.Variant)
	w.WriteTagValue("version", v.Version)
	w.WriteByte('\n')
	w.IndentDecrease()
}

// helper function prints default platform values.
// Including target platform is considered a best-practive.
func printPlatformDefault(w writer) {
	w.WriteTag("platform")
	w.IndentIncrease()
	w.WriteTagValue("os", "linux")
	w.WriteTagValue("arch", "amd64")
	w.WriteByte('\n')
	w.IndentDecrease()
}

// helper function pretty prints the volume sequence.
func printVolumes(w writer, v []*yaml.Volume) {
	w.WriteTag("volumes")
	for _, v := range v {
		s := new(indexWriter)
		s.writer = w
		s.IndentIncrease()

		s.WriteTagValue("name", v.Name)
		if v := v.EmptyDir; v != nil {
			s.WriteTag("temp")
			if isEmptyDirEmpty(v) {
				w.WriteByte(' ')
				w.WriteByte('{')
				w.WriteByte('}')
			} else {
				s.IndentIncrease()
				s.WriteTagValue("medium", v.Medium)
				s.WriteTagValue("size_limit", v.SizeLimit)
				s.IndentDecrease()
			}
		}

		if v := v.HostPath; v != nil {
			s.WriteTag("host")
			s.IndentIncrease()
			s.WriteTagValue("path", v.Path)
			s.IndentDecrease()
		}

		s.IndentDecrease()
	}
}

// helper function pretty prints the workspace block.
func printWorkspace(w writer, v yaml.Workspace) {
	w.WriteTag("workspace")
	w.IndentIncrease()
	w.WriteTagValue("base", v.Base)
	w.WriteTagValue("path", v.Path)
	w.WriteByte('\n')
	w.IndentDecrease()
}

// helper function returns true if the workspace
// object is empty.
func isWorkspaceEmpty(v yaml.Workspace) bool {
	return v.Path == "" && v.Base == ""
}

// helper function returns true if the platform
// object is empty.
func isPlatformEmpty(v yaml.Platform) bool {
	return v.OS == "" &&
		v.Arch == "" &&
		v.Variant == "" &&
		v.Version == ""
}

// helper function returns true if the clone
// object is empty.
func isCloneEmpty(v yaml.Clone) bool {
	return v.Depth == 0 &&
		v.Disable == false &&
		v.SkipVerify == false
}

// helper function returns true if the concurrency
// object is empty.
func isConcurrencyEmpty(v yaml.Concurrency) bool {
	return v.Limit == 0
}

// helper function returns true if the conditions
// object is empty.
func isConditionsEmpty(v yaml.Conditions) bool {
	return isConditionEmpty(v.Action) &&
		isConditionEmpty(v.Branch) &&
		isConditionEmpty(v.Cron) &&
		isConditionEmpty(v.Event) &&
		isConditionEmpty(v.Instance) &&
		isConditionEmpty(v.Paths) &&
		isConditionEmpty(v.Ref) &&
		isConditionEmpty(v.Repo) &&
		isConditionEmpty(v.Status) &&
		isConditionEmpty(v.Target)
}

// helper function returns true if the condition
// object is empty.
func isConditionEmpty(v yaml.Condition) bool {
	return len(v.Exclude) == 0 && len(v.Include) == 0
}

// helper function returns true if the emptydir
// object is empty.
func isEmptyDirEmpty(v *yaml.VolumeEmptyDir) bool {
	return v.SizeLimit == 0 && len(v.Medium) == 0
}
