// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package authority

// GetKnownMetadata returns the known instance discovery metadata for the given
// host, if any. Each call returns a fresh struct with its own Aliases slice,
// so callers may freely modify the result without affecting future calls.
func GetKnownMetadata(host string) (InstanceDiscoveryMetadata, bool) {
	switch host {
	// Public Cloud
	case "login.microsoftonline.com", "login.windows.net", "login.microsoft.com", "sts.windows.net":
		return InstanceDiscoveryMetadata{
			PreferredNetwork: "login.microsoftonline.com",
			PreferredCache:   "login.windows.net",
			Aliases:          []string{"login.microsoftonline.com", "login.windows.net", "login.microsoft.com", "sts.windows.net"},
		}, true
	// China Cloud
	case "login.partner.microsoftonline.cn", "login.chinacloudapi.cn":
		return InstanceDiscoveryMetadata{
			PreferredNetwork: "login.partner.microsoftonline.cn",
			PreferredCache:   "login.partner.microsoftonline.cn",
			Aliases:          []string{"login.partner.microsoftonline.cn", "login.chinacloudapi.cn"},
		}, true
	// Germany Cloud (legacy)
	case "login.microsoftonline.de":
		return InstanceDiscoveryMetadata{
			PreferredNetwork: "login.microsoftonline.de",
			PreferredCache:   "login.microsoftonline.de",
			Aliases:          []string{"login.microsoftonline.de"},
		}, true
	// US Government Cloud
	case "login.microsoftonline.us", "login.usgovcloudapi.net":
		return InstanceDiscoveryMetadata{
			PreferredNetwork: "login.microsoftonline.us",
			PreferredCache:   "login.microsoftonline.us",
			Aliases:          []string{"login.microsoftonline.us", "login.usgovcloudapi.net"},
		}, true
	// US Regional
	case "login-us.microsoftonline.com":
		return InstanceDiscoveryMetadata{
			PreferredNetwork: "login-us.microsoftonline.com",
			PreferredCache:   "login-us.microsoftonline.com",
			Aliases:          []string{"login-us.microsoftonline.com"},
		}, true
	// Bleu (France sovereign cloud)
	case "login.sovcloud-identity.fr":
		return InstanceDiscoveryMetadata{
			PreferredNetwork: "login.sovcloud-identity.fr",
			PreferredCache:   "login.sovcloud-identity.fr",
			Aliases:          []string{"login.sovcloud-identity.fr"},
		}, true
	// Delos (Germany sovereign cloud)
	case "login.sovcloud-identity.de":
		return InstanceDiscoveryMetadata{
			PreferredNetwork: "login.sovcloud-identity.de",
			PreferredCache:   "login.sovcloud-identity.de",
			Aliases:          []string{"login.sovcloud-identity.de"},
		}, true
	// GovSG (Singapore sovereign cloud)
	case "login.sovcloud-identity.sg":
		return InstanceDiscoveryMetadata{
			PreferredNetwork: "login.sovcloud-identity.sg",
			PreferredCache:   "login.sovcloud-identity.sg",
			Aliases:          []string{"login.sovcloud-identity.sg"},
		}, true
	default:
		return InstanceDiscoveryMetadata{}, false
	}
}
