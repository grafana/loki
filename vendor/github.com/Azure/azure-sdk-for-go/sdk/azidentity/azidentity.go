// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"regexp"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
)

const (
	organizationsTenantID   = "organizations"
	developerSignOnClientID = "04b07795-8ddb-461a-bbee-02f9e1bf7b46"
	defaultSuffix           = "/.default"
	tenantIDValidationErr   = "invalid tenantID. You can locate your tenantID by following the instructions listed here: https://docs.microsoft.com/partner-center/find-ids-and-domain-names"
)

const azureAuthorityHost = "AZURE_AUTHORITY_HOST"

// AuthorityHost is the base URL for Azure Active Directory
type AuthorityHost string

const (
	// AzureChina is a global constant to use in order to access the Azure China cloud.
	AzureChina AuthorityHost = "https://login.chinacloudapi.cn/"
	// AzureGovernment is a global constant to use in order to access the Azure Government cloud.
	AzureGovernment AuthorityHost = "https://login.microsoftonline.us/"
	// AzurePublicCloud is a global constant to use in order to access the Azure public cloud.
	AzurePublicCloud AuthorityHost = "https://login.microsoftonline.com/"
)

// setAuthorityHost initializes the authority host for credentials.
func setAuthorityHost(authorityHost AuthorityHost) (string, error) {
	host := string(authorityHost)
	if host == "" {
		host = string(AzurePublicCloud)
		if envAuthorityHost := os.Getenv(azureAuthorityHost); envAuthorityHost != "" {
			host = envAuthorityHost
		}
	}
	u, err := url.Parse(host)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" {
		return "", errors.New("cannot use an authority host without https")
	}
	return host, nil
}

// validTenantID return true is it receives a valid tenantID, returns false otherwise
func validTenantID(tenantID string) bool {
	match, err := regexp.MatchString("^[0-9a-zA-Z-.]+$", tenantID)
	if err != nil {
		return false
	}
	return match
}

func newPipelineAdapter(opts *azcore.ClientOptions) pipelineAdapter {
	pl := runtime.NewPipeline(component, version, runtime.PipelineOptions{}, opts)
	return pipelineAdapter{pl: pl}
}

type pipelineAdapter struct {
	pl runtime.Pipeline
}

func (p pipelineAdapter) CloseIdleConnections() {
	// do nothing
}

func (p pipelineAdapter) Do(r *http.Request) (*http.Response, error) {
	req, err := runtime.NewRequest(r.Context(), r.Method, r.URL.String())
	if err != nil {
		return nil, err
	}
	if r.Body != nil && r.Body != http.NoBody {
		// create a rewindable body from the existing body as required
		var body io.ReadSeekCloser
		if rsc, ok := r.Body.(io.ReadSeekCloser); ok {
			body = rsc
		} else {
			b, err := ioutil.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			body = streaming.NopCloser(bytes.NewReader(b))
		}
		err = req.SetBody(body, r.Header.Get("Content-Type"))
		if err != nil {
			return nil, err
		}
	}
	resp, err := p.pl.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, err
}

// enables fakes for test scenarios
type confidentialClient interface {
	AcquireTokenSilent(ctx context.Context, scopes []string, options ...confidential.AcquireTokenSilentOption) (confidential.AuthResult, error)
	AcquireTokenByAuthCode(ctx context.Context, code string, redirectURI string, scopes []string, options ...confidential.AcquireTokenByAuthCodeOption) (confidential.AuthResult, error)
	AcquireTokenByCredential(ctx context.Context, scopes []string) (confidential.AuthResult, error)
}

// enables fakes for test scenarios
type publicClient interface {
	AcquireTokenSilent(ctx context.Context, scopes []string, options ...public.AcquireTokenSilentOption) (public.AuthResult, error)
	AcquireTokenByUsernamePassword(ctx context.Context, scopes []string, username string, password string) (public.AuthResult, error)
	AcquireTokenByDeviceCode(ctx context.Context, scopes []string) (public.DeviceCode, error)
	AcquireTokenByAuthCode(ctx context.Context, code string, redirectURI string, scopes []string, options ...public.AcquireTokenByAuthCodeOption) (public.AuthResult, error)
	AcquireTokenInteractive(ctx context.Context, scopes []string, options ...public.InteractiveAuthOption) (public.AuthResult, error)
}
