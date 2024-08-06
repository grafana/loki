// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

const (
	credNameAzurePipelines = "AzurePipelinesCredential"
	oidcAPIVersion         = "7.1"
	systemAccessToken      = "SYSTEM_ACCESSTOKEN"
	systemOIDCRequestURI   = "SYSTEM_OIDCREQUESTURI"
)

// azurePipelinesCredential authenticates with workload identity federation in an Azure Pipeline. See
// [Azure Pipelines documentation] for more information.
//
// [Azure Pipelines documentation]: https://learn.microsoft.com/azure/devops/pipelines/library/connect-to-azure?view=azure-devops#create-an-azure-resource-manager-service-connection-that-uses-workload-identity-federation
type azurePipelinesCredential struct {
	connectionID, oidcURI, systemAccessToken string
	cred                                     *ClientAssertionCredential
}

// azurePipelinesCredentialOptions contains optional parameters for AzurePipelinesCredential.
type azurePipelinesCredentialOptions struct {
	azcore.ClientOptions

	// AdditionallyAllowedTenants specifies additional tenants for which the credential may acquire tokens.
	// Add the wildcard value "*" to allow the credential to acquire tokens for any tenant in which the
	// application is registered.
	AdditionallyAllowedTenants []string

	// DisableInstanceDiscovery should be set true only by applications authenticating in disconnected clouds, or
	// private clouds such as Azure Stack. It determines whether the credential requests Microsoft Entra instance metadata
	// from https://login.microsoft.com before authenticating. Setting this to true will skip this request, making
	// the application responsible for ensuring the configured authority is valid and trustworthy.
	DisableInstanceDiscovery bool
}

// newAzurePipelinesCredential is the constructor for AzurePipelinesCredential. In addition to its required arguments,
// it reads a security token for the running build, which is required to authenticate the service connection, from the
// environment variable SYSTEM_ACCESSTOKEN. See the [Azure Pipelines documentation] for an example showing how to set
// this variable in build job YAML.
//
// [Azure Pipelines documentation]: https://learn.microsoft.com/azure/devops/pipelines/build/variables?view=azure-devops&tabs=yaml#systemaccesstoken
func newAzurePipelinesCredential(tenantID, clientID, serviceConnectionID string, options *azurePipelinesCredentialOptions) (*azurePipelinesCredential, error) {
	if options == nil {
		options = &azurePipelinesCredentialOptions{}
	}
	u := os.Getenv(systemOIDCRequestURI)
	if u == "" {
		return nil, fmt.Errorf("no value for environment variable %s. This should be set by Azure Pipelines", systemOIDCRequestURI)
	}
	sat := os.Getenv(systemAccessToken)
	if sat == "" {
		return nil, errors.New("no value for environment variable " + systemAccessToken)
	}
	a := azurePipelinesCredential{
		connectionID:      serviceConnectionID,
		oidcURI:           u,
		systemAccessToken: sat,
	}
	caco := ClientAssertionCredentialOptions{
		AdditionallyAllowedTenants: options.AdditionallyAllowedTenants,
		ClientOptions:              options.ClientOptions,
		DisableInstanceDiscovery:   options.DisableInstanceDiscovery,
	}
	cred, err := NewClientAssertionCredential(tenantID, clientID, a.getAssertion, &caco)
	if err != nil {
		return nil, err
	}
	cred.client.name = credNameAzurePipelines
	a.cred = cred
	return &a, nil
}

// GetToken requests an access token from Microsoft Entra ID. Azure SDK clients call this method automatically.
func (a *azurePipelinesCredential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	var err error
	ctx, endSpan := runtime.StartSpan(ctx, credNameAzurePipelines+"."+traceOpGetToken, a.cred.client.azClient.Tracer(), nil)
	defer func() { endSpan(err) }()
	tk, err := a.cred.GetToken(ctx, opts)
	return tk, err
}

func (a *azurePipelinesCredential) getAssertion(ctx context.Context) (string, error) {
	url := a.oidcURI + "?api-version=" + oidcAPIVersion + "&serviceConnectionId=" + a.connectionID
	url, err := runtime.EncodeQueryParams(url)
	if err != nil {
		return "", newAuthenticationFailedError(credNameAzurePipelines, "couldn't encode OIDC URL: "+err.Error(), nil, nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", newAuthenticationFailedError(credNameAzurePipelines, "couldn't create OIDC token request: "+err.Error(), nil, nil)
	}
	req.Header.Set("Authorization", "Bearer "+a.systemAccessToken)
	res, err := doForClient(a.cred.client.azClient, req)
	if err != nil {
		return "", newAuthenticationFailedError(credNameAzurePipelines, "couldn't send OIDC token request: "+err.Error(), nil, nil)
	}
	if res.StatusCode != http.StatusOK {
		msg := res.Status + " response from the OIDC endpoint. Check service connection ID and Pipeline configuration"
		// include the response because its body, if any, probably contains an error message.
		// OK responses aren't included with errors because they probably contain secrets
		return "", newAuthenticationFailedError(credNameAzurePipelines, msg, res, nil)
	}
	b, err := runtime.Payload(res)
	if err != nil {
		return "", newAuthenticationFailedError(credNameAzurePipelines, "couldn't read OIDC response content: "+err.Error(), nil, nil)
	}
	var r struct {
		OIDCToken string `json:"oidcToken"`
	}
	err = json.Unmarshal(b, &r)
	if err != nil {
		return "", newAuthenticationFailedError(credNameAzurePipelines, "unexpected response from OIDC endpoint", nil, nil)
	}
	return r.OIDCToken, nil
}
