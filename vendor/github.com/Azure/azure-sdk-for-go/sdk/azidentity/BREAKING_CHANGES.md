# Breaking Changes

## v1.15.0

### Service Fabric transport requirements

`ManagedIdentityCredential` now requires a transport that supports the Service Fabric endpoint's
certificate pinning. Applications with a custom `ClientOptions.Transport` must provide an
`*http.Client` whose Transport is `nil` or a standard `*http.Transport` without custom TLS dialing
or verification callbacks. Unsupported transports cause credential construction to fail.
Leave `ClientOptions.Transport` `nil` to use the default transport. The credential derives the
pinned client without modifying a caller-supplied client and rejects redirects.

## v1.8.0

### New errors from `NewManagedIdentityCredential` in some environments

`NewManagedIdentityCredential` now returns an error when `ManagedIdentityCredentialOptions.ID` is set in a hosting environment whose managed identity API doesn't support user-assigned identities. `ManagedIdentityCredential.GetToken()` formerly logged a warning in these cases. Returning an error instead prevents the credential authenticating an unexpected identity. The affected hosting environments are:
  * Azure Arc (user-assigned identities are supported starting in v1.15.0 when the agent supports them)
  * Azure ML (when a resource or object ID is specified; client IDs are supported)
  * Cloud Shell
  * Service Fabric

## v1.6.0

### Behavioral change to `DefaultAzureCredential` in IMDS managed identity scenarios

As of `azidentity` v1.6.0, `DefaultAzureCredential` makes a minor behavioral change when it uses IMDS managed
identity. It sends its first request to IMDS without the "Metadata" header, to expedite validating whether the endpoint
is available. This precedes the credential's first token request and is guaranteed to fail with a 400 error. This error
response can appear in logs but doesn't indicate authentication failed.
