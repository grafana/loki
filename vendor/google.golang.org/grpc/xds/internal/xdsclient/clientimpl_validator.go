/*
 *
 * Copyright 2022 gRPC authors.
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
 *
 */

package xdsclient

import (
	"fmt"

	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

func (c *clientImpl) filterChainUpdateValidator(fc *xdsresource.FilterChain) error {
	if fc == nil {
		return nil
	}
	return c.securityConfigUpdateValidator(fc.SecurityCfg)
}

func (c *clientImpl) securityConfigUpdateValidator(sc *xdsresource.SecurityConfig) error {
	if sc == nil {
		return nil
	}
	if sc.IdentityInstanceName != "" {
		if _, ok := c.config.CertProviderConfigs[sc.IdentityInstanceName]; !ok {
			return fmt.Errorf("identitiy certificate provider instance name %q missing in bootstrap configuration", sc.IdentityInstanceName)
		}
	}
	if sc.RootInstanceName != "" {
		if _, ok := c.config.CertProviderConfigs[sc.RootInstanceName]; !ok {
			return fmt.Errorf("root certificate provider instance name %q missing in bootstrap configuration", sc.RootInstanceName)
		}
	}
	return nil
}

func (c *clientImpl) updateValidator(u interface{}) error {
	switch update := u.(type) {
	case xdsresource.ListenerUpdate:
		if update.InboundListenerCfg == nil || update.InboundListenerCfg.FilterChains == nil {
			return nil
		}
		return update.InboundListenerCfg.FilterChains.Validate(c.filterChainUpdateValidator)
	case xdsresource.ClusterUpdate:
		return c.securityConfigUpdateValidator(update.SecurityCfg)
	default:
		// We currently invoke this update validation function only for LDS and
		// CDS updates. In the future, if we wish to invoke it for other xDS
		// updates, corresponding plumbing needs to be added to those unmarshal
		// functions.
	}
	return nil
}
