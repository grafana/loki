package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	httputil "github.com/aliyun/credentials-go/credentials/internal/http"
)

type URLCredentialsProvider struct {
	url string
	// for sts
	sessionCredentials *sessionCredentials
	// for http options
	httpOptions *HttpOptions
	// inner
	expirationTimestamp int64
}

type URLCredentialsProviderBuilder struct {
	provider *URLCredentialsProvider
}

func NewURLCredentialsProviderBuilder() *URLCredentialsProviderBuilder {
	return &URLCredentialsProviderBuilder{
		provider: &URLCredentialsProvider{},
	}
}

func (builder *URLCredentialsProviderBuilder) WithUrl(url string) *URLCredentialsProviderBuilder {
	builder.provider.url = url
	return builder
}

func (builder *URLCredentialsProviderBuilder) WithHttpOptions(httpOptions *HttpOptions) *URLCredentialsProviderBuilder {
	builder.provider.httpOptions = httpOptions
	return builder
}

func (builder *URLCredentialsProviderBuilder) Build() (provider *URLCredentialsProvider, err error) {

	if builder.provider.url == "" {
		builder.provider.url = os.Getenv("ALIBABA_CLOUD_CREDENTIALS_URI")
	}

	if builder.provider.url == "" {
		err = errors.New("the url is empty")
		return
	}

	provider = builder.provider
	return
}

type urlResponse struct {
	AccessKeyId     *string `json:"AccessKeyId"`
	AccessKeySecret *string `json:"AccessKeySecret"`
	SecurityToken   *string `json:"SecurityToken"`
	Expiration      *string `json:"Expiration"`
}

func (provider *URLCredentialsProvider) getCredentials() (session *sessionCredentials, err error) {
	req := &httputil.Request{
		Method: "GET",
		URL:    provider.url,
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

	res, err := httpDo(req)
	if err != nil {
		return
	}

	if res.StatusCode != http.StatusOK {
		err = fmt.Errorf("get credentials from %s failed: %s", req.BuildRequestURL(), string(res.Body))
		return
	}

	var resp urlResponse
	err = json.Unmarshal(res.Body, &resp)
	if err != nil {
		err = fmt.Errorf("get credentials from %s failed with error, json unmarshal fail: %s", req.BuildRequestURL(), err.Error())
		return
	}

	if resp.AccessKeyId == nil || resp.AccessKeySecret == nil || resp.SecurityToken == nil || resp.Expiration == nil {
		err = fmt.Errorf("refresh credentials from %s failed: %s", req.BuildRequestURL(), string(res.Body))
		return
	}

	session = &sessionCredentials{
		AccessKeyId:     *resp.AccessKeyId,
		AccessKeySecret: *resp.AccessKeySecret,
		SecurityToken:   *resp.SecurityToken,
		Expiration:      *resp.Expiration,
	}
	return
}

func (provider *URLCredentialsProvider) needUpdateCredential() (result bool) {
	if provider.expirationTimestamp == 0 {
		return true
	}

	return provider.expirationTimestamp-time.Now().Unix() <= 180
}

func (provider *URLCredentialsProvider) GetCredentials() (cc *Credentials, err error) {
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

func (provider *URLCredentialsProvider) GetProviderName() string {
	return "credential_uri"
}
