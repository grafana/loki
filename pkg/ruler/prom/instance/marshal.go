package instance

import (
	"bytes"
	"io"

	"gopkg.in/yaml.v2"
)

// UnmarshalConfig unmarshals an instance config from a reader based on a
// provided content type.
func UnmarshalConfig(r io.Reader) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(r)
	dec.SetStrict(true)
	err := dec.Decode(&cfg)
	return &cfg, err
}

// MarshalConfig marshals an instance config based on a provided content type.
func MarshalConfig(c *Config, scrubSecrets bool) ([]byte, error) {
	var buf bytes.Buffer
	err := MarshalConfigToWriter(c, &buf, scrubSecrets)
	return buf.Bytes(), err
}

// MarshalConfigToWriter marshals a config to an io.Writer.
func MarshalConfigToWriter(c *Config, w io.Writer, scrubSecrets bool) error {
	enc := yaml.NewEncoder(w)

	type plain Config
	return enc.Encode((*plain)(c))
}
