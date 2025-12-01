// Copyright 2019 Huawei Technologies Co.,Ltd.
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use
// this file except in compliance with the License.  You may obtain a copy of the
// License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software distributed
// under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
// CONDITIONS OF ANY KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations under the License.

package obs

import (
	"fmt"
	"net/http"
	"strings"
)

// ObsClient defines OBS client.
type ObsClient struct {
	conf       *config
	httpClient *http.Client
}

// New creates a new ObsClient instance.
func New(ak, sk, endpoint string, configurers ...configurer) (*ObsClient, error) {
	conf := &config{endpoint: endpoint}
	conf.securityProviders = make([]securityProvider, 0, 3)
	conf.securityProviders = append(conf.securityProviders, NewBasicSecurityProvider(ak, sk, ""))

	conf.maxRetryCount = -1
	conf.maxRedirectCount = -1
	for _, configurer := range configurers {
		configurer(conf)
	}

	if err := conf.initConfigWithDefault(); err != nil {
		return nil, err
	}
	err := conf.getTransport()
	if err != nil {
		return nil, err
	}

	if isWarnLogEnabled() {
		info := make([]string, 3)
		info[0] = fmt.Sprintf("[OBS SDK Version=%s", OBS_SDK_VERSION)
		info[1] = fmt.Sprintf("Endpoint=%s", conf.endpoint)
		accessMode := "Virtual Hosting"
		if conf.pathStyle {
			accessMode = "Path"
		}
		info[2] = fmt.Sprintf("Access Mode=%s]", accessMode)
		doLog(LEVEL_WARN, strings.Join(info, "];["))
	}

	if conf.httpClient != nil {
		doLog(LEVEL_DEBUG, "Create obsclient with config:\n%s\n", conf)
		obsClient := &ObsClient{conf: conf, httpClient: conf.httpClient}
		return obsClient, nil
	}

	doLog(LEVEL_DEBUG, "Create obsclient with config:\n%s\n", conf)
	obsClient := &ObsClient{conf: conf, httpClient: &http.Client{Transport: conf.transport, CheckRedirect: checkRedirectFunc}}
	return obsClient, nil
}
