package flagext

import (
	"strings"
)

type ConfigFiles []string

// String implements flag.Value
// Format: file1.yaml,file2.yaml
func (cfgFiles *ConfigFiles) String() string {
	return strings.Join(*cfgFiles, ",")
}

// Set implements flag.Value
func (cfgFiles *ConfigFiles) Set(value string) error {
	*cfgFiles = append(*cfgFiles, value)
	return nil
}
