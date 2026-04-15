/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2024 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cors

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/dustin/go-humanize"
)

const defaultXMLNS = "http://s3.amazonaws.com/doc/2006-03-01/"

// Config is the container for a CORS configuration for a bucket.
type Config struct {
	XMLNS     string   `xml:"xmlns,attr,omitempty"`
	XMLName   xml.Name `xml:"CORSConfiguration"`
	CORSRules []Rule   `xml:"CORSRule"`
}

// Rule is a single rule in a CORS configuration.
type Rule struct {
	AllowedHeader []string `xml:"AllowedHeader,omitempty"`
	AllowedMethod []string `xml:"AllowedMethod,omitempty"`
	AllowedOrigin []string `xml:"AllowedOrigin,omitempty"`
	ExposeHeader  []string `xml:"ExposeHeader,omitempty"`
	ID            string   `xml:"ID,omitempty"`
	MaxAgeSeconds int      `xml:"MaxAgeSeconds,omitempty"`
}

// NewConfig creates a new CORS configuration with the given rules.
func NewConfig(rules []Rule) *Config {
	return &Config{
		XMLNS: defaultXMLNS,
		XMLName: xml.Name{
			Local: "CORSConfiguration",
			Space: defaultXMLNS,
		},
		CORSRules: rules,
	}
}

// ParseBucketCorsConfig parses a CORS configuration in XML from an io.Reader.
func ParseBucketCorsConfig(reader io.Reader) (*Config, error) {
	var c Config

	// Max size of cors document is 64KiB according to https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutBucketCors.html
	// This limiter is just for safety so has a max of 128KiB
	err := xml.NewDecoder(io.LimitReader(reader, 128*humanize.KiByte)).Decode(&c)
	if err != nil {
		return nil, fmt.Errorf("decoding xml: %w", err)
	}
	if c.XMLNS == "" {
		c.XMLNS = defaultXMLNS
	}
	for i, rule := range c.CORSRules {
		for j, method := range rule.AllowedMethod {
			c.CORSRules[i].AllowedMethod[j] = strings.ToUpper(method)
		}
	}
	return &c, nil
}

// ToXML marshals the CORS configuration to XML.
func (c Config) ToXML() ([]byte, error) {
	if c.XMLNS == "" {
		c.XMLNS = defaultXMLNS
	}
	data, err := xml.Marshal(&c)
	if err != nil {
		return nil, fmt.Errorf("marshaling xml: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}
