package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	httputil "github.com/aliyun/credentials-go/credentials/internal/http"
	"github.com/aliyun/credentials-go/credentials/internal/utils"
)

// OAuthTokenUpdateCallback 定义OAuth令牌更新回调函数类型
type OAuthTokenUpdateCallback func(refreshToken, accessToken, accessKey, secret, securityToken string, accessTokenExpire, stsExpire int64) error

type oauthCredentialResponse struct {
	AccessKeyId     string `json:"accessKeyId"`
	AccessKeySecret string `json:"accessKeySecret"`
	SecurityToken   string `json:"securityToken"`
	Expiration      string `json:"expiration"`
	RequestId       string `json:"requestId"`
}

type oauthRefreshTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

type OAuthCredentialsProvider struct {
	clientId          string
	signInUrl         string
	refreshToken      string
	accessToken       string
	accessTokenExpire int64

	lastUpdateTimestamp int64
	expirationTimestamp int64
	sessionCredentials  *sessionCredentials
	// for http options
	httpOptions *HttpOptions
	// OAuth token call back
	tokenUpdateCallback OAuthTokenUpdateCallback
}

type OAuthCredentialsProviderBuilder struct {
	provider *OAuthCredentialsProvider
}

func NewOAuthCredentialsProviderBuilder() *OAuthCredentialsProviderBuilder {
	return &OAuthCredentialsProviderBuilder{
		provider: &OAuthCredentialsProvider{},
	}
}

func (b *OAuthCredentialsProviderBuilder) WithClientId(clientId string) *OAuthCredentialsProviderBuilder {
	b.provider.clientId = clientId
	return b
}

func (b *OAuthCredentialsProviderBuilder) WithSignInUrl(signInUrl string) *OAuthCredentialsProviderBuilder {
	b.provider.signInUrl = signInUrl
	return b
}

func (b *OAuthCredentialsProviderBuilder) WithRefreshToken(refreshToken string) *OAuthCredentialsProviderBuilder {
	b.provider.refreshToken = refreshToken
	return b
}

func (b *OAuthCredentialsProviderBuilder) WithAccessToken(accessToken string) *OAuthCredentialsProviderBuilder {
	b.provider.accessToken = accessToken
	return b
}

func (b *OAuthCredentialsProviderBuilder) WithAccessTokenExpire(accessTokenExpire int64) *OAuthCredentialsProviderBuilder {
	b.provider.accessTokenExpire = accessTokenExpire
	return b
}

func (b *OAuthCredentialsProviderBuilder) WithHttpOptions(httpOptions *HttpOptions) *OAuthCredentialsProviderBuilder {
	b.provider.httpOptions = httpOptions
	return b
}

func (b *OAuthCredentialsProviderBuilder) WithTokenUpdateCallback(callback OAuthTokenUpdateCallback) *OAuthCredentialsProviderBuilder {
	b.provider.tokenUpdateCallback = callback
	return b
}

func (b *OAuthCredentialsProviderBuilder) Build() (provider *OAuthCredentialsProvider, err error) {
	if b.provider.clientId == "" {
		err = errors.New("the ClientId is empty")
		return
	}

	if b.provider.signInUrl == "" {
		err = errors.New("the url for sign-in is empty")
		return
	}

	provider = b.provider
	return
}

func (provider *OAuthCredentialsProvider) getCredentials() (session *sessionCredentials, err error) {

	// 仅在 refreshToken 存在时尝试刷新 accessToken
	// 若 refreshToken 不存在，则直接使用当前 accessToken 去交换 accessKeyId，由服务端判断是否有效
	if provider.refreshToken != "" && (provider.accessToken == "" || provider.accessTokenExpire == 0 || provider.accessTokenExpire-time.Now().Unix() <= 1200) {
		err = provider.tryRefreshOauthToken()
		if err != nil {
			return nil, err
		}
	}

	url, err := url.Parse(provider.signInUrl)
	if err != nil {
		return nil, err
	}

	req := &httputil.Request{
		Method:   "POST",
		Protocol: url.Scheme,
		Host:     url.Host,
		Path:     "/v1/exchange",
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

	// set headers
	req.Headers["Content-Type"] = "application/json"
	req.Headers["Authorization"] = fmt.Sprintf("Bearer %s", provider.accessToken)
	res, err := httpDo(req)
	if err != nil {
		return
	}

	if res.StatusCode != http.StatusOK {
		message := "get session token from OAuth failed: "
		err = errors.New(message + string(res.Body))
		return
	}
	var data oauthCredentialResponse
	err = json.Unmarshal(res.Body, &data)
	if err != nil {
		err = fmt.Errorf("get session token from OAuth failed, json.Unmarshal fail: %s", err.Error())
		return
	}

	if data.AccessKeyId == "" || data.AccessKeySecret == "" || data.SecurityToken == "" {
		err = fmt.Errorf("refresh session token err, fail to get credentials from OAuth: " + string(res.Body))
		return
	}

	session = &sessionCredentials{
		AccessKeyId:     data.AccessKeyId,
		AccessKeySecret: data.AccessKeySecret,
		SecurityToken:   data.SecurityToken,
		Expiration:      data.Expiration,
	}
	return
}

func (provider *OAuthCredentialsProvider) tryRefreshOauthToken() (err error) {
	refreshToken := provider.refreshToken
	clientId := provider.clientId

	url, err := url.Parse(provider.signInUrl)
	if err != nil {
		return
	}

	req := &httputil.Request{
		Method:   "POST",
		Protocol: url.Scheme,
		Host:     url.Host,
		Path:     "/v1/token",
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

	bodyForm := make(map[string]string)
	bodyForm["grant_type"] = "refresh_token"
	bodyForm["refresh_token"] = refreshToken
	bodyForm["client_id"] = clientId
	bodyForm["Timestamp"] = utils.GetTimeInFormatISO8601()
	req.Form = bodyForm

	req.Headers["Content-Type"] = "application/x-www-form-urlencoded"
	resp, err := httpDo(req)
	if err != nil {
		return
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to refresh token, status code: %d", resp.StatusCode)
	}
	var tokenResp oauthRefreshTokenResponse
	err = json.Unmarshal(resp.Body, &tokenResp)
	if err != nil {
		err = fmt.Errorf("get refresh token from OAuth failed, json.Unmarshal fail: %s", err.Error())
		return
	}
	if tokenResp.RefreshToken == "" || tokenResp.AccessToken == "" {
		err = fmt.Errorf("failed to refresh token from OAuth: " + string(resp.Body))
		return
	}
	provider.accessToken = tokenResp.AccessToken
	provider.refreshToken = tokenResp.RefreshToken
	provider.accessTokenExpire = time.Now().Unix() + tokenResp.ExpiresIn

	return nil
}

func (provider *OAuthCredentialsProvider) needUpdateCredential() (result bool) {
	if provider.expirationTimestamp == 0 {
		return true
	}

	return provider.expirationTimestamp-time.Now().Unix() <= 180
}

func (provider *OAuthCredentialsProvider) GetCredentials() (cc *Credentials, err error) {
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

		// 如果设置了回调函数，则调用回调函数写回配置文件
		if provider.tokenUpdateCallback != nil {
			err1 := provider.tokenUpdateCallback(provider.refreshToken, provider.accessToken, sessionCredentials.AccessKeyId, sessionCredentials.AccessKeySecret, sessionCredentials.SecurityToken, provider.accessTokenExpire, provider.expirationTimestamp)
			if err1 != nil {
				fmt.Printf("Warning: failed to update OAuth tokens in config file: %v\n", err)
			}
		}
	}

	cc = &Credentials{
		AccessKeyId:     provider.sessionCredentials.AccessKeyId,
		AccessKeySecret: provider.sessionCredentials.AccessKeySecret,
		SecurityToken:   provider.sessionCredentials.SecurityToken,
		ProviderName:    provider.GetProviderName(),
	}
	return
}

func (provider *OAuthCredentialsProvider) GetProviderName() string {
	return "oauth"
}
