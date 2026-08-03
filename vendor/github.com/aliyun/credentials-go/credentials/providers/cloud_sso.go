package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	httputil "github.com/aliyun/credentials-go/credentials/internal/http"
)

type CloudSSOCredentialsProvider struct {
	signInUrl         string
	accountId         string
	accessConfig      string
	accessToken       string
	accessTokenExpire int64

	lastUpdateTimestamp int64
	expirationTimestamp int64
	sessionCredentials  *sessionCredentials
	// for http options
	httpOptions *HttpOptions
}

type CloudSSOCredentialsProviderBuilder struct {
	provider *CloudSSOCredentialsProvider
}

type cloudCredentialOptions struct {
	AccountId             string `json:"AccountId"`
	AccessConfigurationId string `json:"AccessConfigurationId"`
}

type cloudCredentials struct {
	AccessKeyId     string `json:"AccessKeyId"`
	AccessKeySecret string `json:"AccessKeySecret"`
	SecurityToken   string `json:"SecurityToken"`
	Expiration      string `json:"Expiration"`
}

type cloudCredentialResponse struct {
	CloudCredential *cloudCredentials `json:"CloudCredential"`
	RequestId       string            `json:"RequestId"`
}

func NewCloudSSOCredentialsProviderBuilder() *CloudSSOCredentialsProviderBuilder {
	return &CloudSSOCredentialsProviderBuilder{
		provider: &CloudSSOCredentialsProvider{},
	}
}

func (b *CloudSSOCredentialsProviderBuilder) WithSignInUrl(signInUrl string) *CloudSSOCredentialsProviderBuilder {
	b.provider.signInUrl = signInUrl
	return b
}

func (b *CloudSSOCredentialsProviderBuilder) WithAccountId(accountId string) *CloudSSOCredentialsProviderBuilder {
	b.provider.accountId = accountId
	return b
}

func (b *CloudSSOCredentialsProviderBuilder) WithAccessConfig(accessConfig string) *CloudSSOCredentialsProviderBuilder {
	b.provider.accessConfig = accessConfig
	return b
}

func (b *CloudSSOCredentialsProviderBuilder) WithAccessToken(accessToken string) *CloudSSOCredentialsProviderBuilder {
	b.provider.accessToken = accessToken
	return b
}

func (b *CloudSSOCredentialsProviderBuilder) WithAccessTokenExpire(accessTokenExpire int64) *CloudSSOCredentialsProviderBuilder {
	b.provider.accessTokenExpire = accessTokenExpire
	return b
}

func (b *CloudSSOCredentialsProviderBuilder) WithHttpOptions(httpOptions *HttpOptions) *CloudSSOCredentialsProviderBuilder {
	b.provider.httpOptions = httpOptions
	return b
}

func (b *CloudSSOCredentialsProviderBuilder) Build() (provider *CloudSSOCredentialsProvider, err error) {
	if b.provider.accessToken == "" || b.provider.accessTokenExpire == 0 || b.provider.accessTokenExpire-time.Now().Unix() <= 0 {
		err = errors.New("CloudSSO access token is empty or expired, please re-login with cli")
		return
	}

	if b.provider.signInUrl == "" || b.provider.accountId == "" || b.provider.accessConfig == "" {
		err = errors.New("CloudSSO sign in url or account id or access config is empty")
		return
	}

	provider = b.provider
	return
}

func (provider *CloudSSOCredentialsProvider) getCredentials() (session *sessionCredentials, err error) {
	url, err := url.Parse(provider.signInUrl)
	if err != nil {
		return nil, err
	}

	req := &httputil.Request{
		Method:   "POST",
		Protocol: url.Scheme,
		Host:     url.Host,
		Path:     "/cloud-credentials",
		Headers:  map[string]string{},
	}

	connectTimeout := 5 * time.Second
	readTimeout := 10 * time.Second

	if provider.httpOptions != nil && provider.httpOptions.ConnectTimeout > 0 {
		connectTimeout = time.Duration(provider.httpOptions.ConnectTimeout) * time.Millisecond
	}
	if provider.httpOptions != nil && provider.httpOptions.ReadTimeout > 0 {
		readTimeout = time.Duration(provider.httpOptions.ReadTimeout) * time.Millisecond
	}
	if provider.httpOptions != nil && provider.httpOptions.Proxy != "" {
		req.Proxy = provider.httpOptions.Proxy
	}
	req.ConnectTimeout = connectTimeout
	req.ReadTimeout = readTimeout

	body := cloudCredentialOptions{
		AccountId:             provider.accountId,
		AccessConfigurationId: provider.accessConfig,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal options: %w", err)
	}

	req.Body = bodyBytes

	// set headers
	req.Headers["Accept"] = "application/json"
	req.Headers["Content-Type"] = "application/json"
	req.Headers["Authorization"] = fmt.Sprintf("Bearer %s", provider.accessToken)
	res, err := httpDo(req)
	if err != nil {
		return
	}

	if res.StatusCode != http.StatusOK {
		message := "get session token from sso failed: "
		err = errors.New(message + string(res.Body))
		return
	}
	var data cloudCredentialResponse
	err = json.Unmarshal(res.Body, &data)
	if err != nil {
		err = fmt.Errorf("get session token from sso failed, json.Unmarshal fail: %s", err.Error())
		return
	}
	if data.CloudCredential == nil {
		err = fmt.Errorf("get session token from sso failed, fail to get credentials")
		return
	}

	if data.CloudCredential.AccessKeyId == "" || data.CloudCredential.AccessKeySecret == "" || data.CloudCredential.SecurityToken == "" {
		err = fmt.Errorf("refresh session token err, fail to get credentials")
		return
	}

	session = &sessionCredentials{
		AccessKeyId:     data.CloudCredential.AccessKeyId,
		AccessKeySecret: data.CloudCredential.AccessKeySecret,
		SecurityToken:   data.CloudCredential.SecurityToken,
		Expiration:      data.CloudCredential.Expiration,
	}
	return
}

func (provider *CloudSSOCredentialsProvider) needUpdateCredential() (result bool) {
	if provider.expirationTimestamp == 0 {
		return true
	}

	return provider.expirationTimestamp-time.Now().Unix() <= 180
}

func (provider *CloudSSOCredentialsProvider) GetCredentials() (cc *Credentials, err error) {
	if provider.sessionCredentials == nil || provider.needUpdateCredential() {
		sessionCredentials, err1 := provider.getCredentials()
		if err1 != nil {
			return nil, err1
		}

		provider.sessionCredentials = sessionCredentials
		expirationTime, err2 := time.Parse("2006-01-02T15:04:05Z", sessionCredentials.Expiration)
		if err2 != nil {
			return nil, err2
		}

		provider.lastUpdateTimestamp = time.Now().Unix()
		provider.expirationTimestamp = expirationTime.Unix()
	}

	cc = &Credentials{
		AccessKeyId:     provider.sessionCredentials.AccessKeyId,
		AccessKeySecret: provider.sessionCredentials.AccessKeySecret,
		SecurityToken:   provider.sessionCredentials.SecurityToken,
		ProviderName:    provider.GetProviderName(),
	}
	return
}

func (provider *CloudSSOCredentialsProvider) GetProviderName() string {
	return "cloud_sso"
}
