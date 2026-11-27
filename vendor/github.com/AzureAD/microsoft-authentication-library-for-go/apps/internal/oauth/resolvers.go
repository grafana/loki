// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

// TODO(msal): Write some tests. The original code this came from didn't have tests and I'm too
// tired at this point to do it. It, like many other *Manager code I found was broken because
// they didn't have mutex protection.

package oauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth/ops"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/internal/oauth/ops/authority"
	"golang.org/x/sync/singleflight"
)

type cacheEntry struct {
	Endpoints             authority.Endpoints
	ValidForDomainsInList map[string]bool
}

func createcacheEntry(endpoints authority.Endpoints) cacheEntry {
	return cacheEntry{endpoints, map[string]bool{}}
}

// AuthorityEndpoint retrieves endpoints from an authority for auth and token acquisition.
type authorityEndpoint struct {
	rest *ops.REST

	mu    sync.Mutex
	cache map[string]cacheEntry

	resolveGroup singleflight.Group
}

// newAuthorityEndpoint is the constructor for AuthorityEndpoint.
func newAuthorityEndpoint(rest *ops.REST) *authorityEndpoint {
	m := &authorityEndpoint{rest: rest, cache: map[string]cacheEntry{}}
	return m
}

// ResolveEndpoints gets the authorization and token endpoints and creates an AuthorityEndpoints instance
func (m *authorityEndpoint) ResolveEndpoints(ctx context.Context, authorityInfo authority.Info, userPrincipalName string) (authority.Endpoints, error) {

	if endpoints, found := m.cachedEndpoints(authorityInfo, userPrincipalName); found {
		return endpoints, nil
	}

	key := authorityInfo.CanonicalAuthorityURI
	v, err, _ := m.resolveGroup.Do(key, func() (interface{}, error) {
		// Double-check inside the singleflight group: another goroutine may
		// have populated the cache while we were waiting.
		if endpoints, found := m.cachedEndpoints(authorityInfo, userPrincipalName); found {
			return endpoints, nil
		}

		endpoint, metadata, err := m.openIDConfigurationEndpoint(ctx, authorityInfo)
		if err != nil {
			return authority.Endpoints{}, err
		}

		resp, err := m.rest.Authority().GetTenantDiscoveryResponse(ctx, endpoint)
		if err != nil {
			return authority.Endpoints{}, err
		}
		if err := resp.Validate(); err != nil {
			return authority.Endpoints{}, fmt.Errorf("ResolveEndpoints(): %w", err)
		}

		tenant := authorityInfo.Tenant

		endpoints := authority.NewEndpoints(
			strings.ReplaceAll(resp.AuthorizationEndpoint, "{tenant}", tenant),
			strings.ReplaceAll(resp.TokenEndpoint, "{tenant}", tenant),
			strings.ReplaceAll(resp.Issuer, "{tenant}", tenant),
			authorityInfo.Host)

		aliases := aliasesFromMetadata(metadata, authorityInfo.Host)

		if err := resp.ValidateIssuerMatchesAuthority(authorityInfo.CanonicalAuthorityURI,
			aliases); err != nil {
			return authority.Endpoints{}, fmt.Errorf("ResolveEndpoints(): %w", err)
		}

		m.addCachedEndpoints(authorityInfo, userPrincipalName, endpoints)
		return endpoints, nil
	})
	if err != nil {
		return authority.Endpoints{}, err
	}

	return v.(authority.Endpoints), nil
}

// cachedEndpoints returns the cached endpoints if they exist. If not, we return false.
func (m *authorityEndpoint) cachedEndpoints(authorityInfo authority.Info, userPrincipalName string) (authority.Endpoints, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cacheEntry, ok := m.cache[authorityInfo.CanonicalAuthorityURI]; ok {
		if authorityInfo.AuthorityType == authority.ADFS {
			domain, err := adfsDomainFromUpn(userPrincipalName)
			if err == nil {
				if _, ok := cacheEntry.ValidForDomainsInList[domain]; ok {
					return cacheEntry.Endpoints, true
				}
			}
		}
		return cacheEntry.Endpoints, true
	}
	return authority.Endpoints{}, false
}

func aliasesFromMetadata(metadata []authority.InstanceDiscoveryMetadata, authorityHost string) map[string]bool {
	for _, entry := range metadata {
		for _, alias := range entry.Aliases {
			if strings.EqualFold(alias, authorityHost) {
				aliases := make(map[string]bool, len(entry.Aliases))
				for _, alias := range entry.Aliases {
					aliases[alias] = true
				}
				return aliases
			}
		}
	}
	return map[string]bool{}
}

func (m *authorityEndpoint) addCachedEndpoints(authorityInfo authority.Info, userPrincipalName string, endpoints authority.Endpoints) {
	m.mu.Lock()
	defer m.mu.Unlock()

	updatedCacheEntry := createcacheEntry(endpoints)

	if authorityInfo.AuthorityType == authority.ADFS {
		// Since we're here, we've made a call to the backend.  We want to ensure we're caching
		// the latest values from the server.
		if cacheEntry, ok := m.cache[authorityInfo.CanonicalAuthorityURI]; ok {
			for k := range cacheEntry.ValidForDomainsInList {
				updatedCacheEntry.ValidForDomainsInList[k] = true
			}
		}
		domain, err := adfsDomainFromUpn(userPrincipalName)
		if err == nil {
			updatedCacheEntry.ValidForDomainsInList[domain] = true
		}
	}

	m.cache[authorityInfo.CanonicalAuthorityURI] = updatedCacheEntry
}

func (m *authorityEndpoint) openIDConfigurationEndpoint(ctx context.Context, authorityInfo authority.Info) (string, []authority.InstanceDiscoveryMetadata, error) {
	if authorityInfo.AuthorityType == authority.ADFS {
		return fmt.Sprintf("https://%s/adfs/.well-known/openid-configuration", authorityInfo.Host), authorityInfo.InstanceDiscoveryMetadata, nil
	} else if authorityInfo.AuthorityType == authority.DSTS {
		return fmt.Sprintf("https://%s/dstsv2/%s/v2.0/.well-known/openid-configuration", authorityInfo.Host, authority.DSTSTenant), authorityInfo.InstanceDiscoveryMetadata, nil

	} else if authorityInfo.ValidateAuthority && !authority.TrustedHost(authorityInfo.Host) {
		resp, err := m.rest.Authority().AADInstanceDiscovery(ctx, authorityInfo)
		if err != nil {
			return "", nil, err
		}
		return resp.TenantDiscoveryEndpoint, resp.Metadata, nil
	} else if authorityInfo.Region != "" {
		resp, err := m.rest.Authority().AADInstanceDiscovery(ctx, authorityInfo)
		if err != nil {
			return "", nil, err
		}
		return resp.TenantDiscoveryEndpoint, resp.Metadata, nil
	}

	return authorityInfo.CanonicalAuthorityURI + "v2.0/.well-known/openid-configuration", authorityInfo.InstanceDiscoveryMetadata, nil
}

func adfsDomainFromUpn(userPrincipalName string) (string, error) {
	parts := strings.Split(userPrincipalName, "@")
	if len(parts) < 2 {
		return "", errors.New("no @ present in user principal name")
	}
	return parts[1], nil
}
