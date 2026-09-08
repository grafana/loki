// Copyright 2021 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sigv4

import (
	"fmt"
	"regexp"

	"github.com/prometheus/common/config"
)

// sessionNamePattern matches the AWS RoleSessionName constraints:
// 2-64 characters consisting of word characters and +=,.@-
// See: https://docs.aws.amazon.com/STS/latest/APIReference/API_AssumeRole.html
var sessionNamePattern = regexp.MustCompile(`^[\w+=,.@-]{2,64}$`)

// SigV4Config is the configuration for signing remote write requests with
// AWS's SigV4 verification process. Empty values will be retrieved using the
// AWS default credentials chain.
type SigV4Config struct { //nolint:revive
	Region             string            `yaml:"region,omitempty"`
	AccessKey          string            `yaml:"access_key,omitempty"`
	SecretKey          config.Secret     `yaml:"secret_key,omitempty"`
	Profile            string            `yaml:"profile,omitempty"`
	RoleARN            string            `yaml:"role_arn,omitempty"`
	ExternalID         string            `yaml:"external_id,omitempty"`
	SessionName        string            `yaml:"session_name,omitempty"`
	Tags               map[string]string `yaml:"tags,omitempty"`
	UseFIPSSTSEndpoint bool              `yaml:"use_fips_sts_endpoint,omitempty"`
	ServiceName        string            `yaml:"service_name,omitempty"`
}

func (c *SigV4Config) Validate() error {
	if (c.AccessKey == "") != (c.SecretKey == "") {
		return fmt.Errorf("must provide a AWS SigV4 Access key and Secret Key if credentials are specified in the SigV4 config")
	}
	if c.ExternalID != "" && c.RoleARN == "" {
		return fmt.Errorf("external_id can only be used with role_arn")
	}
	if c.SessionName != "" && c.RoleARN == "" {
		return fmt.Errorf("session_name can only be used with role_arn")
	}
	if c.SessionName != "" && !sessionNamePattern.MatchString(c.SessionName) {
		return fmt.Errorf("session_name must match %s (2-64 alphanumeric and +=,.@- characters)", sessionNamePattern.String())
	}
	if len(c.Tags) > 0 && c.RoleARN == "" {
		return fmt.Errorf("tags can only be used with role_arn")
	}
	for k, v := range c.Tags {
		if k == "" {
			return fmt.Errorf("tag key must not be empty")
		}
		if len(k) > 128 {
			return fmt.Errorf("tag key %q exceeds maximum length of 128", k)
		}
		if len(v) > 256 {
			return fmt.Errorf("tag value for key %q exceeds maximum length of 256", k)
		}
	}
	return nil
}

func (c *SigV4Config) UnmarshalYAML(unmarshal func(any) error) error {
	type plain SigV4Config
	*c = SigV4Config{}
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}
	return c.Validate()
}
