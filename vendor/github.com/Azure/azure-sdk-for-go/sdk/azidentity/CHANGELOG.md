# Release History

## 0.13.0 (2022-01-11)

### Breaking Changes
* Replaced `AuthenticationFailedError.RawResponse()` with a field having the same name
* Unexported `CredentialUnavailableError`
* Instances of `ChainedTokenCredential` will now skip looping through the list of source credentials and re-use the first successful credential on subsequent calls to `GetToken`.
  * If `ChainedTokenCredentialOptions.RetrySources` is true, `ChainedTokenCredential` will continue to try all of the originally provided credentials each time the `GetToken` method is called.
  * `ChainedTokenCredential.successfulCredential` will contain a reference to the last successful credential.
  * `DefaultAzureCredenial` will also re-use the first successful credential on subsequent calls to `GetToken`.
  * `DefaultAzureCredential.chain.successfulCredential` will also contain a reference to the last successful credential.

### Other Changes
* `ManagedIdentityCredential` no longer probes IMDS before requesting a token
  from it. Also, an error response from IMDS no longer disables a credential
  instance. Following an error, a credential instance will continue to send
  requests to IMDS as necessary.
* Adopted MSAL for user and service principal authentication
* Updated `azcore` requirement to 0.21.0

## 0.12.0 (2021-11-02)
### Breaking Changes
* Raised minimum go version to 1.16
* Removed `NewAuthenticationPolicy()` from credentials. Clients should instead use azcore's
 `runtime.NewBearerTokenPolicy()` to construct a bearer token authorization policy.
* The `AuthorityHost` field in credential options structs is now a custom type,
  `AuthorityHost`, with underlying type `string`
* `NewChainedTokenCredential` has a new signature to accommodate a placeholder
  options struct:
  ```go
  // before
  cred, err := NewChainedTokenCredential(credA, credB)

  // after
  cred, err := NewChainedTokenCredential([]azcore.TokenCredential{credA, credB}, nil)
  ```
* Removed `ExcludeAzureCLICredential`, `ExcludeEnvironmentCredential`, and `ExcludeMSICredential`
  from `DefaultAzureCredentialOptions`
* `NewClientCertificateCredential` requires a `[]*x509.Certificate` and `crypto.PrivateKey` instead of
  a path to a certificate file. Added `ParseCertificates` to simplify getting these in common cases:
  ```go
  // before
  cred, err := NewClientCertificateCredential("tenant", "client-id", "/cert.pem", nil)

  // after
  certData, err := os.ReadFile("/cert.pem")
  certs, key, err := ParseCertificates(certData, password)
  cred, err := NewClientCertificateCredential(tenantID, clientID, certs, key, nil)
  ```
* Removed `InteractiveBrowserCredentialOptions.ClientSecret` and `.Port`
* Removed `AADAuthenticationFailedError`
* Removed `id` parameter of `NewManagedIdentityCredential()`. User assigned identities are now
  specified by `ManagedIdentityCredentialOptions.ID`:
  ```go
  // before
  cred, err := NewManagedIdentityCredential("client-id", nil)
  // or, for a resource ID
  opts := &ManagedIdentityCredentialOptions{ID: ResourceID}
  cred, err := NewManagedIdentityCredential("/subscriptions/...", opts)

  // after
  clientID := ClientID("7cf7db0d-...")
  opts := &ManagedIdentityCredentialOptions{ID: clientID}
  // or, for a resource ID
  resID: ResourceID("/subscriptions/...")
  opts := &ManagedIdentityCredentialOptions{ID: resID}
  cred, err := NewManagedIdentityCredential(opts)
  ```
* `DeviceCodeCredentialOptions.UserPrompt` has a new type: `func(context.Context, DeviceCodeMessage) error`
* Credential options structs now embed `azcore.ClientOptions`. In addition to changing literal initialization
  syntax, this change renames `HTTPClient` fields to `Transport`.
* Renamed `LogCredential` to `EventCredential`
* `AzureCLICredential` no longer reads the environment variable `AZURE_CLI_PATH`
* `NewManagedIdentityCredential` no longer reads environment variables `AZURE_CLIENT_ID` and
  `AZURE_RESOURCE_ID`. Use `ManagedIdentityCredentialOptions.ID` instead.
* Unexported `AuthenticationFailedError` and `CredentialUnavailableError` structs. In their place are two
  interfaces having the same names.

### Bugs Fixed
* `AzureCLICredential.GetToken` no longer mutates its `opts.Scopes`

### Features Added
* Added connection configuration options to `DefaultAzureCredentialOptions`
* `AuthenticationFailedError.RawResponse()` returns the HTTP response motivating the error,
  if available

### Other Changes
* `NewDefaultAzureCredential()` returns `*DefaultAzureCredential` instead of `*ChainedTokenCredential`
* Added `TenantID` field to `DefaultAzureCredentialOptions` and `AzureCLICredentialOptions`

## 0.11.0 (2021-09-08)
### Breaking Changes
* Unexported `AzureCLICredentialOptions.TokenProvider` and its type,
  `AzureCLITokenProvider`

### Bug Fixes
* `ManagedIdentityCredential.GetToken` returns `CredentialUnavailableError`
  when IMDS has no assigned identity, signaling `DefaultAzureCredential` to
  try other credentials


## 0.10.0 (2021-08-30)
### Breaking Changes
* Update based on `azcore` refactor [#15383](https://github.com/Azure/azure-sdk-for-go/pull/15383)

## 0.9.3 (2021-08-20)

### Bugs Fixed
* `ManagedIdentityCredential.GetToken` no longer mutates its `opts.Scopes`

### Other Changes
* Bumps version of `azcore` to `v0.18.1`


## 0.9.2 (2021-07-23)
### Features Added
* Adding support for Service Fabric environment in `ManagedIdentityCredential`
* Adding an option for using a resource ID instead of client ID in `ManagedIdentityCredential`


## 0.9.1 (2021-05-24)
### Features Added
* Add LICENSE.txt and bump version information


## 0.9.0 (2021-05-21)
### Features Added
* Add support for authenticating in Azure Stack environments
* Enable user assigned identities for the IMDS scenario in `ManagedIdentityCredential`
* Add scope to resource conversion in `GetToken()` on `ManagedIdentityCredential`


## 0.8.0 (2021-01-20)
### Features Added
* Updating documentation


## 0.7.1 (2021-01-04)
### Features Added
* Adding port option to `InteractiveBrowserCredential`


## 0.7.0 (2020-12-11)
### Features Added
* Add `redirectURI` parameter back to authentication code flow


## 0.6.1 (2020-12-09)
### Features Added
* Updating query parameter in `ManagedIdentityCredential` and updating datetime string for parsing managed identity access tokens.


## 0.6.0 (2020-11-16)
### Features Added
* Remove `RedirectURL` parameter from auth code flow to align with the MSAL implementation which relies on the native client redirect URL.


## 0.5.0 (2020-10-30)
### Features Added
* Flattening credential options


## 0.4.3 (2020-10-21)
### Features Added
* Adding Azure Arc support in `ManagedIdentityCredential`


## 0.4.2 (2020-10-16)
### Features Added
* Typo fixes


## 0.4.1 (2020-10-16)
### Features Added
* Ensure authority hosts are only HTTPs


## 0.4.0 (2020-10-16)
### Features Added
* Adding options structs for credentials


## 0.3.0 (2020-10-09)
### Features Added
* Update `DeviceCodeCredential` callback


## 0.2.2 (2020-10-09)
### Features Added
* Add `AuthorizationCodeCredential`


## 0.2.1 (2020-10-06)
### Features Added
* Add `InteractiveBrowserCredential`


## 0.2.0 (2020-09-11)
### Features Added
* Refactor `azidentity` on top of `azcore` refactor
* Updated policies to conform to `policy.Policy` interface changes.
* Updated non-retriable errors to conform to `azcore.NonRetriableError`.
* Fixed calls to `Request.SetBody()` to include content type.
* Switched endpoints to string types and removed extra parsing code.


## 0.1.1 (2020-09-02)
### Features Added
* Add `AzureCLICredential` to `DefaultAzureCredential` chain


## 0.1.0 (2020-07-23)
### Features Added
* Initial Release. Azure Identity library that provides Azure Active Directory token authentication support for the SDK.
