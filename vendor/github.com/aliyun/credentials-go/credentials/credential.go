package credentials

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/alibabacloud-go/debug/debug"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/credentials-go/credentials/internal/utils"
	"github.com/aliyun/credentials-go/credentials/providers"
	"github.com/aliyun/credentials-go/credentials/request"
	"github.com/aliyun/credentials-go/credentials/response"
)

var debuglog = debug.Init("credential")

var hookParse = func(err error) error {
	return err
}

// Credential is an interface for getting actual credential
type Credential interface {
	// Deprecated: GetAccessKeyId is deprecated, use GetCredential instead of.
	GetAccessKeyId() (*string, error)
	// Deprecated: GetAccessKeySecret is deprecated, use GetCredential instead of.
	GetAccessKeySecret() (*string, error)
	// Deprecated: GetSecurityToken is deprecated, use GetCredential instead of.
	GetSecurityToken() (*string, error)
	GetBearerToken() *string
	GetType() *string
	GetCredential() (*CredentialModel, error)
}

// Config is important when call NewCredential
type Config struct {
	// Credential type, including access_key, sts, bearer, ecs_ram_role, ram_role_arn, rsa_key_pair, oidc_role_arn, credentials_uri
	Type            *string `json:"type"`
	AccessKeyId     *string `json:"access_key_id"`
	AccessKeySecret *string `json:"access_key_secret"`
	SecurityToken   *string `json:"security_token"`
	BearerToken     *string `json:"bearer_token"`

	// Used when the type is ram_role_arn or oidc_role_arn
	OIDCProviderArn       *string `json:"oidc_provider_arn"`
	OIDCTokenFilePath     *string `json:"oidc_token"`
	RoleArn               *string `json:"role_arn"`
	RoleSessionName       *string `json:"role_session_name"`
	RoleSessionExpiration *int    `json:"role_session_expiration"`
	Policy                *string `json:"policy"`
	ExternalId            *string `json:"external_id"`
	STSEndpoint           *string `json:"sts_endpoint"`

	// Used when the type is ecs_ram_role
	RoleName *string `json:"role_name"`
	// Deprecated
	EnableIMDSv2  *bool `json:"enable_imds_v2"`
	DisableIMDSv1 *bool `json:"disable_imds_v1"`
	// Deprecated
	MetadataTokenDuration *int `json:"metadata_token_duration"`

	// Used when the type is credentials_uri
	Url *string `json:"url"`

	// Deprecated
	// Used when the type is rsa_key_pair
	SessionExpiration *int    `json:"session_expiration"`
	PublicKeyId       *string `json:"public_key_id"`
	PrivateKeyFile    *string `json:"private_key_file"`
	Host              *string `json:"host"`

	// Read timeout, in milliseconds.
	// The default value for ecs_ram_role is 1000ms, the default value for ram_role_arn is 5000ms, and the default value for oidc_role_arn is 5000ms.
	Timeout *int `json:"timeout"`
	// Connection timeout, in milliseconds.
	// The default value for ecs_ram_role is 1000ms, the default value for ram_role_arn is 10000ms, and the default value for oidc_role_arn is 10000ms.
	ConnectTimeout *int `json:"connect_timeout"`

	Proxy          *string  `json:"proxy"`
	InAdvanceScale *float64 `json:"inAdvanceScale"`
}

func (s Config) String() string {
	return tea.Prettify(s)
}

func (s Config) GoString() string {
	return s.String()
}

func (s *Config) SetAccessKeyId(v string) *Config {
	s.AccessKeyId = &v
	return s
}

func (s *Config) SetAccessKeySecret(v string) *Config {
	s.AccessKeySecret = &v
	return s
}

func (s *Config) SetSecurityToken(v string) *Config {
	s.SecurityToken = &v
	return s
}

func (s *Config) SetRoleArn(v string) *Config {
	s.RoleArn = &v
	return s
}

func (s *Config) SetRoleSessionName(v string) *Config {
	s.RoleSessionName = &v
	return s
}

func (s *Config) SetPublicKeyId(v string) *Config {
	s.PublicKeyId = &v
	return s
}

func (s *Config) SetRoleName(v string) *Config {
	s.RoleName = &v
	return s
}

func (s *Config) SetEnableIMDSv2(v bool) *Config {
	s.EnableIMDSv2 = &v
	return s
}

func (s *Config) SetDisableIMDSv1(v bool) *Config {
	s.DisableIMDSv1 = &v
	return s
}

func (s *Config) SetMetadataTokenDuration(v int) *Config {
	s.MetadataTokenDuration = &v
	return s
}

func (s *Config) SetSessionExpiration(v int) *Config {
	s.SessionExpiration = &v
	return s
}

func (s *Config) SetPrivateKeyFile(v string) *Config {
	s.PrivateKeyFile = &v
	return s
}

func (s *Config) SetBearerToken(v string) *Config {
	s.BearerToken = &v
	return s
}

func (s *Config) SetRoleSessionExpiration(v int) *Config {
	s.RoleSessionExpiration = &v
	return s
}

func (s *Config) SetPolicy(v string) *Config {
	s.Policy = &v
	return s
}

func (s *Config) SetHost(v string) *Config {
	s.Host = &v
	return s
}

func (s *Config) SetTimeout(v int) *Config {
	s.Timeout = &v
	return s
}

func (s *Config) SetConnectTimeout(v int) *Config {
	s.ConnectTimeout = &v
	return s
}

func (s *Config) SetProxy(v string) *Config {
	s.Proxy = &v
	return s
}

func (s *Config) SetType(v string) *Config {
	s.Type = &v
	return s
}

func (s *Config) SetOIDCTokenFilePath(v string) *Config {
	s.OIDCTokenFilePath = &v
	return s
}

func (s *Config) SetOIDCProviderArn(v string) *Config {
	s.OIDCProviderArn = &v
	return s
}

func (s *Config) SetURLCredential(v string) *Config {
	if v == "" {
		v = os.Getenv("ALIBABA_CLOUD_CREDENTIALS_URI")
	}
	s.Url = &v
	return s
}

func (s *Config) SetSTSEndpoint(v string) *Config {
	s.STSEndpoint = &v
	return s
}

func (s *Config) SetExternalId(v string) *Config {
	s.ExternalId = &v
	return s
}

// NewCredential return a credential according to the type in config.
// if config is nil, the function will use default provider chain to get credentials.
// please see README.md for detail.
func NewCredential(config *Config) (credential Credential, err error) {
	if config == nil {
		provider := providers.NewDefaultCredentialsProvider()
		credential = FromCredentialsProvider("default", provider)
		return
	}
	switch tea.StringValue(config.Type) {
	case "credentials_uri":
		provider, err := providers.NewURLCredentialsProviderBuilder().
			WithUrl(tea.StringValue(config.Url)).
			WithHttpOptions(&providers.HttpOptions{
				Proxy:          tea.StringValue(config.Proxy),
				ReadTimeout:    tea.IntValue(config.Timeout),
				ConnectTimeout: tea.IntValue(config.ConnectTimeout),
			}).
			Build()

		if err != nil {
			return nil, err
		}
		credential = FromCredentialsProvider("credentials_uri", provider)
	case "oidc_role_arn":
		provider, err := providers.NewOIDCCredentialsProviderBuilder().
			WithRoleArn(tea.StringValue(config.RoleArn)).
			WithOIDCTokenFilePath(tea.StringValue(config.OIDCTokenFilePath)).
			WithOIDCProviderARN(tea.StringValue(config.OIDCProviderArn)).
			WithDurationSeconds(tea.IntValue(config.RoleSessionExpiration)).
			WithPolicy(tea.StringValue(config.Policy)).
			WithRoleSessionName(tea.StringValue(config.RoleSessionName)).
			WithSTSEndpoint(tea.StringValue(config.STSEndpoint)).
			WithHttpOptions(&providers.HttpOptions{
				Proxy:          tea.StringValue(config.Proxy),
				ReadTimeout:    tea.IntValue(config.Timeout),
				ConnectTimeout: tea.IntValue(config.ConnectTimeout),
			}).
			Build()

		if err != nil {
			return nil, err
		}
		credential = FromCredentialsProvider("oidc_role_arn", provider)
	case "access_key":
		provider, err := providers.NewStaticAKCredentialsProviderBuilder().
			WithAccessKeyId(tea.StringValue(config.AccessKeyId)).
			WithAccessKeySecret(tea.StringValue(config.AccessKeySecret)).
			Build()
		if err != nil {
			return nil, err
		}

		credential = FromCredentialsProvider("access_key", provider)
	case "sts":
		provider, err := providers.NewStaticSTSCredentialsProviderBuilder().
			WithAccessKeyId(tea.StringValue(config.AccessKeyId)).
			WithAccessKeySecret(tea.StringValue(config.AccessKeySecret)).
			WithSecurityToken(tea.StringValue(config.SecurityToken)).
			Build()
		if err != nil {
			return nil, err
		}

		credential = FromCredentialsProvider("sts", provider)
	case "ecs_ram_role":
		provider, err := providers.NewECSRAMRoleCredentialsProviderBuilder().
			WithRoleName(tea.StringValue(config.RoleName)).
			WithDisableIMDSv1(tea.BoolValue(config.DisableIMDSv1)).
			Build()

		if err != nil {
			return nil, err
		}

		credential = FromCredentialsProvider("ecs_ram_role", provider)
	case "ram_role_arn":
		var credentialsProvider providers.CredentialsProvider
		if config.SecurityToken != nil && *config.SecurityToken != "" {
			credentialsProvider, err = providers.NewStaticSTSCredentialsProviderBuilder().
				WithAccessKeyId(tea.StringValue(config.AccessKeyId)).
				WithAccessKeySecret(tea.StringValue(config.AccessKeySecret)).
				WithSecurityToken(tea.StringValue(config.SecurityToken)).
				Build()
		} else {
			credentialsProvider, err = providers.NewStaticAKCredentialsProviderBuilder().
				WithAccessKeyId(tea.StringValue(config.AccessKeyId)).
				WithAccessKeySecret(tea.StringValue(config.AccessKeySecret)).
				Build()
		}

		if err != nil {
			return nil, err
		}

		provider, err := providers.NewRAMRoleARNCredentialsProviderBuilder().
			WithCredentialsProvider(credentialsProvider).
			WithRoleArn(tea.StringValue(config.RoleArn)).
			WithRoleSessionName(tea.StringValue(config.RoleSessionName)).
			WithPolicy(tea.StringValue(config.Policy)).
			WithDurationSeconds(tea.IntValue(config.RoleSessionExpiration)).
			WithExternalId(tea.StringValue(config.ExternalId)).
			WithStsEndpoint(tea.StringValue(config.STSEndpoint)).
			WithHttpOptions(&providers.HttpOptions{
				Proxy:          tea.StringValue(config.Proxy),
				ReadTimeout:    tea.IntValue(config.Timeout),
				ConnectTimeout: tea.IntValue(config.ConnectTimeout),
			}).
			Build()
		if err != nil {
			return nil, err
		}

		credential = FromCredentialsProvider("ram_role_arn", provider)
	case "rsa_key_pair":
		err = checkRSAKeyPair(config)
		if err != nil {
			return
		}
		file, err1 := os.Open(tea.StringValue(config.PrivateKeyFile))
		if err1 != nil {
			err = fmt.Errorf("InvalidPath: Can not open PrivateKeyFile, err is %s", err1.Error())
			return
		}
		defer file.Close()
		var privateKey string
		scan := bufio.NewScanner(file)
		for scan.Scan() {
			if strings.HasPrefix(scan.Text(), "----") {
				continue
			}
			privateKey += scan.Text() + "\n"
		}
		runtime := &utils.Runtime{
			Host:           tea.StringValue(config.Host),
			Proxy:          tea.StringValue(config.Proxy),
			ReadTimeout:    tea.IntValue(config.Timeout),
			ConnectTimeout: tea.IntValue(config.ConnectTimeout),
			STSEndpoint:    tea.StringValue(config.STSEndpoint),
		}
		credential = newRsaKeyPairCredential(
			privateKey,
			tea.StringValue(config.PublicKeyId),
			tea.IntValue(config.SessionExpiration),
			runtime)
	case "bearer":
		if tea.StringValue(config.BearerToken) == "" {
			err = errors.New("BearerToken cannot be empty")
			return
		}
		credential = newBearerTokenCredential(tea.StringValue(config.BearerToken))
	default:
		err = errors.New("invalid type option, support: access_key, sts, bearer, ecs_ram_role, ram_role_arn, rsa_key_pair, oidc_role_arn, credentials_uri")
		return
	}
	return credential, nil
}

func checkRSAKeyPair(config *Config) (err error) {
	if tea.StringValue(config.PrivateKeyFile) == "" {
		err = errors.New("PrivateKeyFile cannot be empty")
		return
	}
	if tea.StringValue(config.PublicKeyId) == "" {
		err = errors.New("PublicKeyId cannot be empty")
		return
	}
	return
}

func doAction(request *request.CommonRequest, runtime *utils.Runtime) (content []byte, err error) {
	var urlEncoded string
	if request.BodyParams != nil {
		urlEncoded = utils.GetURLFormedMap(request.BodyParams)
	}
	httpRequest, err := http.NewRequest(request.Method, request.URL, strings.NewReader(urlEncoded))
	if err != nil {
		return
	}
	httpRequest.Proto = "HTTP/1.1"
	httpRequest.Host = request.Domain
	debuglog("> %s %s %s", httpRequest.Method, httpRequest.URL.RequestURI(), httpRequest.Proto)
	debuglog("> Host: %s", httpRequest.Host)
	for key, value := range request.Headers {
		if value != "" {
			debuglog("> %s: %s", key, value)
			httpRequest.Header[key] = []string{value}
		}
	}
	debuglog(">")
	httpClient := &http.Client{}
	httpClient.Timeout = time.Duration(runtime.ReadTimeout) * time.Second
	proxy := &url.URL{}
	if runtime.Proxy != "" {
		proxy, err = url.Parse(runtime.Proxy)
		if err != nil {
			return
		}
	}
	transport := &http.Transport{}
	if proxy != nil && runtime.Proxy != "" {
		transport.Proxy = http.ProxyURL(proxy)
	}
	transport.DialContext = utils.Timeout(time.Duration(runtime.ConnectTimeout) * time.Second)
	httpClient.Transport = transport
	httpResponse, err := hookDo(httpClient.Do)(httpRequest)
	if err != nil {
		return
	}
	debuglog("< %s %s", httpResponse.Proto, httpResponse.Status)
	for key, value := range httpResponse.Header {
		debuglog("< %s: %v", key, strings.Join(value, ""))
	}
	debuglog("<")

	resp := &response.CommonResponse{}
	err = hookParse(resp.ParseFromHTTPResponse(httpResponse))
	if err != nil {
		return
	}
	debuglog("%s", resp.GetHTTPContentString())
	if resp.GetHTTPStatus() != http.StatusOK {
		err = fmt.Errorf("httpStatus: %d, message = %s", resp.GetHTTPStatus(), resp.GetHTTPContentString())
		return
	}
	return resp.GetHTTPContentBytes(), nil
}

type credentialsProviderWrap struct {
	typeName string
	provider providers.CredentialsProvider
}

// Deprecated: use GetCredential() instead of
func (cp *credentialsProviderWrap) GetAccessKeyId() (accessKeyId *string, err error) {
	cc, err := cp.provider.GetCredentials()
	if err != nil {
		return
	}
	accessKeyId = &cc.AccessKeyId
	return
}

// Deprecated: use GetCredential() instead of
func (cp *credentialsProviderWrap) GetAccessKeySecret() (accessKeySecret *string, err error) {
	cc, err := cp.provider.GetCredentials()
	if err != nil {
		return
	}
	accessKeySecret = &cc.AccessKeySecret
	return
}

// Deprecated: use GetCredential() instead of
func (cp *credentialsProviderWrap) GetSecurityToken() (securityToken *string, err error) {
	cc, err := cp.provider.GetCredentials()
	if err != nil {
		return
	}
	securityToken = &cc.SecurityToken
	return
}

// Deprecated: don't use it
func (cp *credentialsProviderWrap) GetBearerToken() (bearerToken *string) {
	return tea.String("")
}

// Get credentials
func (cp *credentialsProviderWrap) GetCredential() (cm *CredentialModel, err error) {
	c, err := cp.provider.GetCredentials()
	if err != nil {
		return
	}

	cm = &CredentialModel{
		AccessKeyId:     &c.AccessKeyId,
		AccessKeySecret: &c.AccessKeySecret,
		SecurityToken:   &c.SecurityToken,
		Type:            &cp.typeName,
		ProviderName:    &c.ProviderName,
	}
	return
}

func (cp *credentialsProviderWrap) GetType() *string {
	return &cp.typeName
}

func FromCredentialsProvider(typeName string, cp providers.CredentialsProvider) Credential {
	return &credentialsProviderWrap{
		typeName: typeName,
		provider: cp,
	}
}
