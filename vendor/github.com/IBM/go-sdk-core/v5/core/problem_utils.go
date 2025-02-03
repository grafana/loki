package core

// (C) Copyright IBM Corp. 2024.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"
)

func ComputeConsoleMessage(o OrderableProblem) string {
	return getProblemInfoAsYAML(o.GetConsoleOrderedMaps())
}

func ComputeDebugMessage(o OrderableProblem) string {
	return getProblemInfoAsYAML(o.GetDebugOrderedMaps())
}

// CreateIDHash computes a unique ID based on a given prefix
// and problem attribute fields.
func CreateIDHash(prefix string, fields ...string) string {
	signature := strings.Join(fields, "")
	hash := sha256.Sum256([]byte(signature))
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(hash[:4]))
}

// getProblemInfoAsYAML formats the ordered problem data as
// YAML for human/machine readable printing.
func getProblemInfoAsYAML(orderedMaps *OrderedMaps) string {
	asYaml, err := yaml.Marshal(orderedMaps.GetMaps())

	if err != nil {
		return fmt.Sprintf("Error serializing the problem information: %s", err.Error())
	}
	return fmt.Sprintf("---\n%s---\n", asYaml)
}

// getComponentInfo is a convenient way to access the name of the
// component alongside the current semantic version of the component.
func getComponentInfo() *ProblemComponent {
	return NewProblemComponent(MODULE_NAME, __VERSION__)
}

// is provides a simple utility function that assists problem types
// implement an "Is" function for checking error equality. Error types
// are treated as equivalent if they are both Problem types and their
// IDs match.
func is(target error, id string) bool {
	var problem Problem
	return errors.As(target, &problem) && problem.GetID() == id
}
