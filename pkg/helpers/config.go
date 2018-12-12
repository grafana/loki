package helpers

import (
	"io/ioutil"

	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v2"
)

// LoadConfig read YAML-formatted config from filename into cfg.
func LoadConfig(filename string, cfg interface{}) error {
	buf, err := ioutil.ReadFile(filename)
	if err != nil {
		return errors.Wrap(err, "Error reading config file")
	}

	return yaml.UnmarshalStrict(buf, cfg)
}
