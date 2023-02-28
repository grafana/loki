// Copyright 2016 The Prometheus Authors
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

package remote

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"

	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier"
)

type Storage struct {
	queriers []DetailQuerier
}

type DetailQuerier interface {
	querier.Querier
	SelectLogDetails(context.Context, logql.SelectLogParams) (iter.EntryIterator, loghttp.Streams, error)
}

func (s *Storage) Queriers() []DetailQuerier {
	return s.queriers
}
func (s *Storage) ApplyConf(remoteReadConfigs []ReadConfig) error {
	readHashes := make(map[string]struct{})
	queriers := make([]DetailQuerier, 0, len(remoteReadConfigs))
	for _, rrConf := range remoteReadConfigs {
		hash, err := toHash(rrConf)
		if err != nil {
			return err
		}

		// Don't allow duplicate remote read configs.
		if _, ok := readHashes[hash]; ok {
			return fmt.Errorf("duplicate remote read configs are not allowed, found duplicate for URL: %s", rrConf.URL)
		}
		readHashes[hash] = struct{}{}

		// Set the queue name to the config hash if the user has not set
		// a name in their remote write config so we can still differentiate
		// between queues that have the same remote write endpoint.
		name := hash[:6]
		if rrConf.Name != "" {
			name = rrConf.Name
		}
		querier, err := NewQuerier(name, rrConf)
		if err != nil {
			return err
		}
		queriers = append(queriers, querier)
	}
	s.queriers = queriers
	return nil
}

// Used for hashing configs and diff'ing hashes in ApplyConfig.
func toHash(data interface{}) (string, error) {
	bytes, err := yaml.Marshal(data)
	if err != nil {
		return "", err
	}
	hash := md5.Sum(bytes)
	return hex.EncodeToString(hash[:]), nil
}
