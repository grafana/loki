package credentials

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/credentials-go/credentials/internal/utils"
	"github.com/aliyun/credentials-go/credentials/request"
)

// URLCredential is a kind of credential
type URLCredentialsProvider struct {
	URL string
	*credentialUpdater
	*sessionCredential
	runtime *utils.Runtime
}

type URLResponse struct {
	AccessKeyId     string `json:"AccessKeyId" xml:"AccessKeyId"`
	AccessKeySecret string `json:"AccessKeySecret" xml:"AccessKeySecret"`
	SecurityToken   string `json:"SecurityToken" xml:"SecurityToken"`
	Expiration      string `json:"Expiration" xml:"Expiration"`
}

func newURLCredential(URL string) *URLCredentialsProvider {
	credentialUpdater := new(credentialUpdater)
	if URL == "" {
		URL = os.Getenv("ALIBABA_CLOUD_CREDENTIALS_URI")
	}
	return &URLCredentialsProvider{
		URL:               URL,
		credentialUpdater: credentialUpdater,
	}
}

func (e *URLCredentialsProvider) GetCredential() (*CredentialModel, error) {
	if e.sessionCredential == nil || e.needUpdateCredential() {
		err := e.updateCredential()
		if err != nil {
			return nil, err
		}
	}
	credential := &CredentialModel{
		AccessKeyId:     tea.String(e.sessionCredential.AccessKeyId),
		AccessKeySecret: tea.String(e.sessionCredential.AccessKeySecret),
		SecurityToken:   tea.String(e.sessionCredential.SecurityToken),
		Type:            tea.String("credential_uri"),
	}
	return credential, nil
}

// GetAccessKeyId reutrns  URLCredential's AccessKeyId
// if AccessKeyId is not exist or out of date, the function will update it.
func (e *URLCredentialsProvider) GetAccessKeyId() (accessKeyId *string, err error) {
	c, err := e.GetCredential()
	if err != nil {
		return
	}
	accessKeyId = c.AccessKeyId
	return
}

// GetAccessSecret reutrns  URLCredential's AccessKeySecret
// if AccessKeySecret is not exist or out of date, the function will update it.
func (e *URLCredentialsProvider) GetAccessKeySecret() (accessKeySecret *string, err error) {
	c, err := e.GetCredential()
	if err != nil {
		return
	}
	accessKeySecret = c.AccessKeySecret
	return
}

// GetSecurityToken reutrns  URLCredential's SecurityToken
// if SecurityToken is not exist or out of date, the function will update it.
func (e *URLCredentialsProvider) GetSecurityToken() (securityToken *string, err error) {
	c, err := e.GetCredential()
	if err != nil {
		return
	}
	securityToken = c.SecurityToken
	return
}

// GetBearerToken is useless for URLCredential
func (e *URLCredentialsProvider) GetBearerToken() *string {
	return tea.String("")
}

// GetType reutrns  URLCredential's type
func (e *URLCredentialsProvider) GetType() *string {
	return tea.String("credential_uri")
}

func (e *URLCredentialsProvider) updateCredential() (err error) {
	if e.runtime == nil {
		e.runtime = new(utils.Runtime)
	}
	request := request.NewCommonRequest()
	request.URL = e.URL
	request.Method = "GET"
	content, err := doAction(request, e.runtime)
	if err != nil {
		return fmt.Errorf("get credentials from %s failed with error: %s", e.URL, err.Error())
	}
	var resp *URLResponse
	err = json.Unmarshal(content, &resp)
	if err != nil {
		return fmt.Errorf("get credentials from %s failed with error, json unmarshal fail: %s", e.URL, err.Error())
	}
	if resp.AccessKeyId == "" || resp.AccessKeySecret == "" || resp.SecurityToken == "" || resp.Expiration == "" {
		return fmt.Errorf("get credentials failed: AccessKeyId: %s, AccessKeySecret: %s, SecurityToken: %s, Expiration: %s", resp.AccessKeyId, resp.AccessKeySecret, resp.SecurityToken, resp.Expiration)
	}

	expirationTime, err := time.Parse("2006-01-02T15:04:05Z", resp.Expiration)
	e.lastUpdateTimestamp = time.Now().Unix()
	e.credentialExpiration = int(expirationTime.Unix() - time.Now().Unix())
	e.sessionCredential = &sessionCredential{
		AccessKeyId:     resp.AccessKeyId,
		AccessKeySecret: resp.AccessKeySecret,
		SecurityToken:   resp.SecurityToken,
	}

	return
}
