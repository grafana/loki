package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	httputil "github.com/aliyun/credentials-go/credentials/internal/http"
)

type ECSRAMRoleCredentialsProvider struct {
	roleName      string
	disableIMDSv1 bool
	// for sts
	session             *sessionCredentials
	expirationTimestamp int64
	// for http options
	httpOptions *HttpOptions
}

type ECSRAMRoleCredentialsProviderBuilder struct {
	provider *ECSRAMRoleCredentialsProvider
}

func NewECSRAMRoleCredentialsProviderBuilder() *ECSRAMRoleCredentialsProviderBuilder {
	return &ECSRAMRoleCredentialsProviderBuilder{
		provider: &ECSRAMRoleCredentialsProvider{},
	}
}

func (builder *ECSRAMRoleCredentialsProviderBuilder) WithRoleName(roleName string) *ECSRAMRoleCredentialsProviderBuilder {
	builder.provider.roleName = roleName
	return builder
}

func (builder *ECSRAMRoleCredentialsProviderBuilder) WithDisableIMDSv1(disableIMDSv1 bool) *ECSRAMRoleCredentialsProviderBuilder {
	builder.provider.disableIMDSv1 = disableIMDSv1
	return builder
}

func (builder *ECSRAMRoleCredentialsProviderBuilder) WithHttpOptions(httpOptions *HttpOptions) *ECSRAMRoleCredentialsProviderBuilder {
	builder.provider.httpOptions = httpOptions
	return builder
}

const defaultMetadataTokenDuration = 21600 // 6 hours

func (builder *ECSRAMRoleCredentialsProviderBuilder) Build() (provider *ECSRAMRoleCredentialsProvider, err error) {

	if strings.ToLower(os.Getenv("ALIBABA_CLOUD_ECS_METADATA_DISABLED")) == "true" {
		err = errors.New("IMDS credentials is disabled")
		return
	}

	// 设置 roleName 默认值
	if builder.provider.roleName == "" {
		builder.provider.roleName = os.Getenv("ALIBABA_CLOUD_ECS_METADATA")
	}

	if !builder.provider.disableIMDSv1 {
		builder.provider.disableIMDSv1 = strings.ToLower(os.Getenv("ALIBABA_CLOUD_IMDSV1_DISABLED")) == "true"
	}

	provider = builder.provider
	return
}

type ecsRAMRoleResponse struct {
	Code            *string `json:"Code"`
	AccessKeyId     *string `json:"AccessKeyId"`
	AccessKeySecret *string `json:"AccessKeySecret"`
	SecurityToken   *string `json:"SecurityToken"`
	LastUpdated     *string `json:"LastUpdated"`
	Expiration      *string `json:"Expiration"`
}

func (provider *ECSRAMRoleCredentialsProvider) needUpdateCredential() bool {
	if provider.expirationTimestamp == 0 {
		return true
	}

	return provider.expirationTimestamp-time.Now().Unix() <= 180
}

func (provider *ECSRAMRoleCredentialsProvider) getRoleName() (roleName string, err error) {
	req := &httputil.Request{
		Method:   "GET",
		Protocol: "http",
		Host:     "100.100.100.200",
		Path:     "/latest/meta-data/ram/security-credentials/",
		Headers:  map[string]string{},
	}

	connectTimeout := 1 * time.Second
	readTimeout := 1 * time.Second

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

	metadataToken, err := provider.getMetadataToken()
	if err != nil {
		return "", err
	}
	if metadataToken != "" {
		req.Headers["x-aliyun-ecs-metadata-token"] = metadataToken
	}

	res, err := httpDo(req)
	if err != nil {
		err = fmt.Errorf("get role name failed: %s", err.Error())
		return
	}

	if res.StatusCode != 200 {
		err = fmt.Errorf("get role name failed: %s %d", req.BuildRequestURL(), res.StatusCode)
		return
	}

	roleName = strings.TrimSpace(string(res.Body))
	return
}

func (provider *ECSRAMRoleCredentialsProvider) getCredentials() (session *sessionCredentials, err error) {
	roleName := provider.roleName
	if roleName == "" {
		roleName, err = provider.getRoleName()
		if err != nil {
			return
		}
	}

	req := &httputil.Request{
		Method:   "GET",
		Protocol: "http",
		Host:     "100.100.100.200",
		Path:     "/latest/meta-data/ram/security-credentials/" + roleName,
		Headers:  map[string]string{},
	}

	connectTimeout := 1 * time.Second
	readTimeout := 1 * time.Second

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

	metadataToken, err := provider.getMetadataToken()
	if err != nil {
		return nil, err
	}
	if metadataToken != "" {
		req.Headers["x-aliyun-ecs-metadata-token"] = metadataToken
	}

	res, err := httpDo(req)
	if err != nil {
		err = fmt.Errorf("refresh Ecs sts token err: %s", err.Error())
		return
	}

	if res.StatusCode != 200 {
		err = fmt.Errorf("refresh Ecs sts token err, httpStatus: %d, message = %s", res.StatusCode, string(res.Body))
		return
	}

	var data ecsRAMRoleResponse
	err = json.Unmarshal(res.Body, &data)
	if err != nil {
		err = fmt.Errorf("refresh Ecs sts token err, json.Unmarshal fail: %s", err.Error())
		return
	}

	if data.AccessKeyId == nil || data.AccessKeySecret == nil || data.SecurityToken == nil {
		err = fmt.Errorf("refresh Ecs sts token err, fail to get credentials")
		return
	}

	if *data.Code != "Success" {
		err = fmt.Errorf("refresh Ecs sts token err, Code is not Success")
		return
	}

	session = &sessionCredentials{
		AccessKeyId:     *data.AccessKeyId,
		AccessKeySecret: *data.AccessKeySecret,
		SecurityToken:   *data.SecurityToken,
		Expiration:      *data.Expiration,
	}
	return
}

func (provider *ECSRAMRoleCredentialsProvider) GetCredentials() (cc *Credentials, err error) {
	if provider.session == nil || provider.needUpdateCredential() {
		session, err1 := provider.getCredentials()
		if err1 != nil {
			return nil, err1
		}

		provider.session = session
		expirationTime, err2 := time.Parse("2006-01-02T15:04:05Z", session.Expiration)
		if err2 != nil {
			return nil, err2
		}
		provider.expirationTimestamp = expirationTime.Unix()
	}

	cc = &Credentials{
		AccessKeyId:     provider.session.AccessKeyId,
		AccessKeySecret: provider.session.AccessKeySecret,
		SecurityToken:   provider.session.SecurityToken,
		ProviderName:    provider.GetProviderName(),
	}
	return
}

func (provider *ECSRAMRoleCredentialsProvider) GetProviderName() string {
	return "ecs_ram_role"
}

func (provider *ECSRAMRoleCredentialsProvider) getMetadataToken() (metadataToken string, err error) {
	// PUT http://100.100.100.200/latest/api/token
	req := &httputil.Request{
		Method:   "PUT",
		Protocol: "http",
		Host:     "100.100.100.200",
		Path:     "/latest/api/token",
		Headers: map[string]string{
			"X-aliyun-ecs-metadata-token-ttl-seconds": strconv.Itoa(defaultMetadataTokenDuration),
		},
	}

	connectTimeout := 1 * time.Second
	readTimeout := 1 * time.Second

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

	res, _err := httpDo(req)
	if _err != nil {
		if provider.disableIMDSv1 {
			err = fmt.Errorf("get metadata token failed: %s", _err.Error())
		}
		return
	}
	if res.StatusCode != 200 {
		if provider.disableIMDSv1 {
			err = fmt.Errorf("refresh Ecs sts token err, httpStatus: %d, message = %s", res.StatusCode, string(res.Body))
		}
		return
	}
	metadataToken = string(res.Body)
	return
}
