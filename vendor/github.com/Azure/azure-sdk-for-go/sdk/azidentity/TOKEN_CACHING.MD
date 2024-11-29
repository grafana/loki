## Token caching in the Azure Identity client module

*Token caching* is a feature provided by the Azure Identity library that allows apps to:

- Improve their resilience and performance.
- Reduce the number of requests made to Microsoft Entra ID to obtain access tokens.
- Reduce the number of times the user is prompted to authenticate.

When an app needs to access a protected Azure resource, it typically needs to obtain an access token from Entra ID. Obtaining that token involves sending a request to Entra ID and may also involve prompting the user. Entra ID then validates the credentials provided in the request and issues an access token.

Token caching, via the Azure Identity library, allows the app to store this access token [in memory](#in-memory-token-caching), where it's accessible to the current process, or [on disk](#persistent-token-caching) where it can be accessed across application or process invocations. The token can then be retrieved quickly and easily the next time the app needs to access the same resource. The app can avoid making another request to Entra ID, which reduces network traffic and improves resilience. Additionally, in scenarios where the app is authenticating users, token caching also avoids prompting the user each time new tokens are requested.

### In-memory token caching

*In-memory token caching* is the default option provided by the Azure Identity library. This caching approach allows apps to store access tokens in memory. With in-memory token caching, the library first determines if a valid access token for the requested resource is already stored in memory. If a valid token is found, it's returned to the app without the need to make another request to Entra ID. If a valid token isn't found, the library will automatically acquire a token by sending a request to Entra ID. The in-memory token cache provided by the Azure Identity library is thread-safe.

**Note:** When Azure Identity library credentials are used with Azure service libraries (for example, Azure Blob Storage), the in-memory token caching is active in the `Pipeline` layer as well. All `TokenCredential` implementations are supported there, including custom implementations external to the Azure Identity library.

#### Caching cannot be disabled

As there are many levels of caching, it's not possible disable in-memory caching. However, the in-memory cache may be cleared by creating a new credential instance.

### Persistent token caching

> Only azidentity v1.5.0-beta versions support persistent token caching

*Persistent disk token caching* is an opt-in feature in the Azure Identity library. The feature allows apps to cache access tokens in an encrypted, persistent storage mechanism. As indicated in the following table, the storage mechanism differs across operating systems.

| Operating system | Storage mechanism                     |
|------------------|---------------------------------------|
| Linux            | kernel key retention service (keyctl) |
| macOS            | Keychain                              |
| Windows          | DPAPI                                 |

By default the token cache will protect any data which is persisted using the user data protection APIs available on the current platform.
However, there are cases where no data protection is available, and applications may choose to allow storing the token cache in an unencrypted state by setting `TokenCachePersistenceOptions.AllowUnencryptedStorage` to `true`. This allows a credential to fall back to unencrypted storage if it can't encrypt the cache. However, we do not recommend using this storage method due to its significantly lower security measures. In addition, tokens are not encrypted solely to the current user, which could potentially allow unauthorized access to the cache by individuals with machine access.

With persistent disk token caching enabled, the library first determines if a valid access token for the requested resource is already stored in the persistent cache. If a valid token is found, it's returned to the app without the need to make another request to Entra ID. Additionally, the tokens are preserved across app runs, which:

- Makes the app more resilient to failures.
- Ensures the app can continue to function during an Entra ID outage or disruption.
- Avoids having to prompt users to authenticate each time the process is restarted.

>IMPORTANT! The token cache contains sensitive data and **MUST** be protected to prevent compromising accounts. All application decisions regarding the persistence of the token cache must consider that a breach of its content will fully compromise all the accounts it contains.

#### Example code

See the [package documentation](https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/azidentity@v1.6.0-beta.2#pkg-overview) for example code demonstrating how to configure persistent caching and access cached data.

### Credentials supporting token caching

The following table indicates the state of in-memory and persistent caching in each credential type.

**Note:** In-memory caching is activated by default. Persistent token caching needs to be enabled as shown in [this example](https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/azidentity@v1.5.0-beta.1#example-package-PersistentCache).

| Credential                     | In-memory token caching                                             | Persistent token caching |
|--------------------------------|---------------------------------------------------------------------|--------------------------|
| `AzureCLICredential`           | Not Supported                                                       | Not Supported            |
| `AzureDeveloperCLICredential`  | Not Supported                                                       | Not Supported            |
| `AzurePipelinesCredential`     | Supported                                                           | Supported                |
| `ClientAssertionCredential`    | Supported                                                           | Supported                |
| `ClientCertificateCredential`  | Supported                                                           | Supported                |
| `ClientSecretCredential`       | Supported                                                           | Supported                |
| `DefaultAzureCredential`       | Supported if the target credential in the default chain supports it | Not Supported            |
| `DeviceCodeCredential`         | Supported                                                           | Supported                |
| `EnvironmentCredential`        | Supported                                                           | Not Supported            |
| `InteractiveBrowserCredential` | Supported                                                           | Supported                |
| `ManagedIdentityCredential`    | Supported                                                           | Not Supported            |
| `OnBehalfOfCredential`         | Supported                                                           | Supported                |
| `UsernamePasswordCredential`   | Supported                                                           | Supported                |
| `WorkloadIdentityCredential`   | Supported                                                           | Supported                |
