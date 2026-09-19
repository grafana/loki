// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/log"
)

const (
	credNameAzurePowerShell = "AzurePowerShellCredential"
	noAzAccountModule       = "Az.Accounts module not found"
)

// AzurePowerShellCredentialOptions contains optional parameters for AzurePowerShellCredential.
type AzurePowerShellCredentialOptions struct {
	// AdditionallyAllowedTenants specifies tenants to which the credential may authenticate, in addition to
	// TenantID. When TenantID is empty, this option has no effect and the credential will authenticate to
	// any requested tenant. Add the wildcard value "*" to allow the credential to authenticate to any tenant.
	AdditionallyAllowedTenants []string

	// TenantID identifies the tenant the credential should authenticate in.
	// Defaults to Azure PowerShell's default tenant, which is typically the home tenant of the logged in user.
	TenantID string

	// inDefaultChain is true when the credential is part of DefaultAzureCredential
	inDefaultChain bool

	// exec is used by tests to fake invoking Azure PowerShell
	exec executor
}

// AzurePowerShellCredential authenticates as the identity logged in to Azure PowerShell.
type AzurePowerShellCredential struct {
	mu   *sync.Mutex
	opts AzurePowerShellCredentialOptions
}

// NewAzurePowerShellCredential constructs an AzurePowerShellCredential. Pass nil to accept default options.
func NewAzurePowerShellCredential(options *AzurePowerShellCredentialOptions) (*AzurePowerShellCredential, error) {
	cp := AzurePowerShellCredentialOptions{}

	if options != nil {
		cp = *options
	}

	if cp.TenantID != "" && !validTenantID(cp.TenantID) {
		return nil, errInvalidTenantID
	}

	if cp.exec == nil {
		cp.exec = shellExec
	}

	cp.AdditionallyAllowedTenants = resolveAdditionalTenants(cp.AdditionallyAllowedTenants)

	return &AzurePowerShellCredential{mu: &sync.Mutex{}, opts: cp}, nil
}

// GetToken requests a token from Azure PowerShell. This credential doesn't cache tokens, so every call invokes Azure PowerShell.
// This method is called automatically by Azure SDK clients.
func (c *AzurePowerShellCredential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	at := azcore.AccessToken{}

	if len(opts.Scopes) != 1 {
		return at, errors.New(credNameAzurePowerShell + ": GetToken() requires exactly one scope")
	}

	if !validScope(opts.Scopes[0]) {
		return at, fmt.Errorf("%s.GetToken(): invalid scope %q", credNameAzurePowerShell, opts.Scopes[0])
	}

	tenant, err := resolveTenant(c.opts.TenantID, opts.TenantID, credNameAzurePowerShell, c.opts.AdditionallyAllowedTenants)
	if err != nil {
		return at, err
	}

	// Always pass a Microsoft Entra ID v1 resource URI (not a v2 scope) because Get-AzAccessToken only supports v1 resource URIs.
	resource := strings.TrimSuffix(opts.Scopes[0], defaultSuffix)

	tenantArg := ""
	if tenant != "" {
		tenantArg = fmt.Sprintf(" -TenantId '%s'", tenant)
	}

	if opts.Claims != "" {
		encoded := base64.StdEncoding.EncodeToString([]byte(opts.Claims))
		return at, fmt.Errorf(
			"%s.GetToken(): Azure PowerShell requires multifactor authentication or additional claims. Run this command then retry the operation: Connect-AzAccount%s -ClaimsChallenge '%s'",
			credNameAzurePowerShell,
			tenantArg,
			encoded,
		)
	}

	// Inline script to handle Get-AzAccessToken differences between Az.Accounts versions with SecureString handling and minimum version requirement
	script := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
[version]$minimumVersion = '2.2.0'

$mod = Import-Module Az.Accounts -MinimumVersion $minimumVersion -PassThru -ErrorAction SilentlyContinue

if (-not $mod) {
    Write-Error '%s'
}

$params = @{
    ResourceUrl   = '%s'
	WarningAction = 'Ignore'
}

# Only force AsSecureString for Az.Accounts versions > 2.17.0 and < 5.0.0 which return plain text token by default.
# Newer Az.Accounts versions return SecureString token by default and no longer use AsSecureString parameter.
if ($mod.Version -ge [version]'2.17.0' -and $mod.Version -lt [version]'5.0.0') {
    $params['AsSecureString'] = $true
}

$tenantId = '%s'
if ($tenantId.Length -gt 0) {
    $params['TenantId'] = '%s'
}

$token = Get-AzAccessToken @params

$customToken = New-Object -TypeName psobject

# The following .NET interop pattern is supported in all PowerShell versions and safely converts SecureString to plain text.
if ($token.Token -is [System.Security.SecureString]) {
    $ssPtr = [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($token.Token)
    try {
        $plainToken = [System.Runtime.InteropServices.Marshal]::PtrToStringBSTR($ssPtr)
    } finally {
        [System.Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ssPtr)
    }
    $customToken | Add-Member -MemberType NoteProperty -Name Token -Value $plainToken
} else {
    $customToken | Add-Member -MemberType NoteProperty -Name Token -Value $token.Token
}
$customToken | Add-Member -MemberType NoteProperty -Name ExpiresOn -Value $token.ExpiresOn.ToUnixTimeSeconds()

$jsonToken = $customToken | ConvertTo-Json
return $jsonToken
`, noAzAccountModule, resource, tenant, tenant)

	// Windows: prefer pwsh.exe (PowerShell Core), fallback to powershell.exe (Windows PowerShell)
	// Unix: only support pwsh (PowerShell Core)
	exe := "pwsh"
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh.exe"); err == nil {
			exe = "pwsh.exe"
		} else {
			exe = "powershell.exe"
		}
	}

	command := exe + " -NoProfile -NonInteractive -OutputFormat Text -EncodedCommand " + base64EncodeUTF16LE(script)

	c.mu.Lock()
	defer c.mu.Unlock()

	b, err := c.opts.exec(ctx, credNameAzurePowerShell, command)
	if err == nil {
		at, err = c.createAccessToken(b)
	}

	if err != nil {
		err = unavailableIfInDAC(err, c.opts.inDefaultChain)
		return at, err
	}

	msg := fmt.Sprintf("%s.GetToken() acquired a token for scope %q", credNameAzurePowerShell, strings.Join(opts.Scopes, ", "))
	log.Write(EventAuthentication, msg)

	return at, nil
}

func (c *AzurePowerShellCredential) createAccessToken(tk []byte) (azcore.AccessToken, error) {
	t := struct {
		Token     string `json:"Token"`
		ExpiresOn int64  `json:"ExpiresOn"`
	}{}

	err := json.Unmarshal(tk, &t)
	if err != nil {
		return azcore.AccessToken{}, err
	}

	converted := azcore.AccessToken{
		Token:     t.Token,
		ExpiresOn: time.Unix(t.ExpiresOn, 0).UTC(),
	}

	return converted, nil
}

// Encodes a string to Base64 using UTF-16LE encoding
func base64EncodeUTF16LE(text string) string {
	u16 := utf16.Encode([]rune(text))
	buf := make([]byte, len(u16)*2)
	for i, v := range u16 {
		binary.LittleEndian.PutUint16(buf[i*2:], v)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// Decodes a Base64 UTF-16LE string back to string
func base64DecodeUTF16LE(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	u16 := make([]uint16, len(data)/2)
	for i := range u16 {
		u16[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	return string(utf16.Decode(u16)), nil
}

var _ azcore.TokenCredential = (*AzurePowerShellCredential)(nil)
