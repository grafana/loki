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
	"sort"
	"strings"

	"github.com/drone/drone-yaml/yaml"
)

// TODO consider "!!binary |" for secret value

// helper function to pretty prints the signature resource.
func printSecret(w writer, v *yaml.Secret) {
	w.WriteString("---")
	w.WriteTagValue("version", v.Version)
	w.WriteTagValue("kind", v.Kind)
	w.WriteTagValue("type", v.Type)

	if len(v.Data) > 0 {
		w.WriteTagValue("name", v.Name)
		printData(w, v.Data)
	}
	if isSecretGetEmpty(v.Get) == false {
		w.WriteTagValue("name", v.Name)
		w.WriteByte('\n')
		printGet(w, v.Get)
	}
	w.WriteByte('\n')
	w.WriteByte('\n')
}

// helper function prints the get block.
func printGet(w writer, v yaml.SecretGet) {
	w.WriteTag("get")
	w.IndentIncrease()
	w.WriteTagValue("path", v.Path)
	w.WriteTagValue("name", v.Name)
	w.WriteTagValue("key", v.Key)
	w.IndentDecrease()
}

// helper function prints the external data.
func printExternalData(w writer, d map[string]yaml.ExternalData) {
	var keys []string
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	w.WriteTag("external_data")
	w.IndentIncrease()
	for _, k := range keys {
		v := d[k]
		w.WriteTag(k)
		w.IndentIncrease()
		w.WriteTagValue("path", v.Path)
		w.WriteTagValue("name", v.Name)
		w.IndentDecrease()
	}
	w.IndentDecrease()
}

func printData(w writer, d string) {
	w.WriteTag("data")
	w.WriteByte(' ')
	w.WriteByte('>')
	w.IndentIncrease()
	d = spaceReplacer.Replace(d)
	for _, s := range chunk(d, 60) {
		w.WriteByte('\n')
		w.Indent()
		w.WriteString(s)
	}
	w.IndentDecrease()
}

// replace spaces and newlines.
var spaceReplacer = strings.NewReplacer(" ", "", "\n", "")

// helper function returns true if the secret get
// object is empty.
func isSecretGetEmpty(v yaml.SecretGet) bool {
	return v.Key == "" &&
		v.Name == "" &&
		v.Path == ""
}
