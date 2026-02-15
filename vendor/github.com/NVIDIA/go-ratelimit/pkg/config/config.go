/*
 * Copyright (c) 2025, NVIDIA CORPORATION.  All rights reserved.
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
package config

import (
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Logging             LoggingConfig             `yaml:"logging"`
}

type LoggingConfig struct {
	ConsoleLevel string `yaml:"console_level"`
	FileLevel    string `yaml:"file_level"`
	File         string `yaml:"file"`
}

var config Config

// LoadConfig reads the configuration from a YAML file
func LoadConfig(configFile string) (Config, error) {

	data, err := os.ReadFile(configFile)

	if err != nil {
		return config, err
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return config, err
	}

	return config, nil
}
