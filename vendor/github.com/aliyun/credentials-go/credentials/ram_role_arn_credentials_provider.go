package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/credentials-go/credentials/internal/utils"
	"github.com/aliyun/credentials-go/credentials/request"
)

const defaultDurationSeconds = 3600

// RAMRoleArnCredentialsProvider is a kind of credentials
type RAMRoleArnCredentialsProvider struct {
	*credentialUpdater
	AccessKeyId           string
	AccessKeySecret       string
	SecurityToken         string
	RoleArn               string
	RoleSessionName       string
	RoleSessionExpiration int
	Policy                string
	ExternalId            string
	sessionCredential     *sessionCredential
	runtime               *utils.Runtime
}

type ramRoleArnResponse struct {
	Credentials *credentialsInResponse `json:"Credentials" xml:"Credentials"`
}

type credentialsInResponse struct {
	AccessKeyId     string `json:"AccessKeyId" xml:"AccessKeyId"`
	AccessKeySecret string `json:"AccessKeySecret" xml:"AccessKeySecret"`
	SecurityToken   string `json:"SecurityToken" xml:"SecurityToken"`
	Expiration      string `json:"Expiration" xml:"Expiration"`
}

func newRAMRoleArnl(accessKeyId, accessKeySecret, securityToken, roleArn, roleSessionName, policy string, roleSessionExpiration int, externalId string, runtime *utils.Runtime) *RAMRoleArnCredentialsProvider {
	return &RAMRoleArnCredentialsProvider{
		AccessKeyId:           accessKeyId,
		AccessKeySecret:       accessKeySecret,
		SecurityToken:         securityToken,
		RoleArn:               roleArn,
		RoleSessionName:       roleSessionName,
		RoleSessionExpiration: roleSessionExpiration,
		Policy:                policy,
		ExternalId:            externalId,
		credentialUpdater:     new(credentialUpdater),
		runtime:               runtime,
	}
}

func newRAMRoleArnCredential(accessKeyId, accessKeySecret, roleArn, roleSessionName, policy string, roleSessionExpiration int, runtime *utils.Runtime) *RAMRoleArnCredentialsProvider {
	return &RAMRoleArnCredentialsProvider{
		AccessKeyId:           accessKeyId,
		AccessKeySecret:       accessKeySecret,
		RoleArn:               roleArn,
		RoleSessionName:       roleSessionName,
		RoleSessionExpiration: roleSessionExpiration,
		Policy:                policy,
		credentialUpdater:     new(credentialUpdater),
		runtime:               runtime,
	}
}

func newRAMRoleArnWithExternalIdCredential(accessKeyId, accessKeySecret, roleArn, roleSessionName, policy string, roleSessionExpiration int, externalId string, runtime *utils.Runtime) *RAMRoleArnCredentialsProvider {
	return &RAMRoleArnCredentialsProvider{
		AccessKeyId:           accessKeyId,
		AccessKeySecret:       accessKeySecret,
		RoleArn:               roleArn,
		RoleSessionName:       roleSessionName,
		RoleSessionExpiration: roleSessionExpiration,
		Policy:                policy,
		ExternalId:            externalId,
		credentialUpdater:     new(credentialUpdater),
		runtime:               runtime,
	}
}

func (e *RAMRoleArnCredentialsProvider) GetCredential() (*CredentialModel, error) {
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
		Type:            tea.String("ram_role_arn"),
	}
	return credential, nil
}

// GetAccessKeyId reutrns RAMRoleArnCredentialsProvider's AccessKeyId
// if AccessKeyId is not exist or out of date, the function will update it.
func (r *RAMRoleArnCredentialsProvider) GetAccessKeyId() (accessKeyId *string, err error) {
	c, err := r.GetCredential()
	if err != nil {
		return
	}

	accessKeyId = c.AccessKeyId
	return
}

// GetAccessSecret reutrns RAMRoleArnCredentialsProvider's AccessKeySecret
// if AccessKeySecret is not exist or out of date, the function will update it.
func (r *RAMRoleArnCredentialsProvider) GetAccessKeySecret() (accessKeySecret *string, err error) {
	c, err := r.GetCredential()
	if err != nil {
		return
	}

	accessKeySecret = c.AccessKeySecret
	return
}

// GetSecurityToken reutrns RAMRoleArnCredentialsProvider's SecurityToken
// if SecurityToken is not exist or out of date, the function will update it.
func (r *RAMRoleArnCredentialsProvider) GetSecurityToken() (securityToken *string, err error) {
	c, err := r.GetCredential()
	if err != nil {
		return
	}

	securityToken = c.SecurityToken
	return
}

// GetBearerToken is useless RAMRoleArnCredentialsProvider
func (r *RAMRoleArnCredentialsProvider) GetBearerToken() *string {
	return tea.String("")
}

// GetType reutrns RAMRoleArnCredentialsProvider's type
func (r *RAMRoleArnCredentialsProvider) GetType() *string {
	return tea.String("ram_role_arn")
}

func (r *RAMRoleArnCredentialsProvider) updateCredential() (err error) {
	if r.runtime == nil {
		r.runtime = new(utils.Runtime)
	}
	request := request.NewCommonRequest()
	request.Domain = "sts.aliyuncs.com"
	if r.runtime.STSEndpoint != "" {
		request.Domain = r.runtime.STSEndpoint
	}
	request.Scheme = "HTTPS"
	request.Method = "GET"
	request.QueryParams["AccessKeyId"] = r.AccessKeyId
	if r.SecurityToken != "" {
		request.QueryParams["SecurityToken"] = r.SecurityToken
	}
	request.QueryParams["Action"] = "AssumeRole"
	request.QueryParams["Format"] = "JSON"
	if r.RoleSessionExpiration > 0 {
		if r.RoleSessionExpiration >= 900 && r.RoleSessionExpiration <= 3600 {
			request.QueryParams["DurationSeconds"] = strconv.Itoa(r.RoleSessionExpiration)
		} else {
			err = errors.New("[InvalidParam]:Assume Role session duration should be in the range of 15min - 1Hr")
			return
		}
	} else {
		request.QueryParams["DurationSeconds"] = strconv.Itoa(defaultDurationSeconds)
	}
	request.QueryParams["RoleArn"] = r.RoleArn
	if r.Policy != "" {
		request.QueryParams["Policy"] = r.Policy
	}
	if r.ExternalId != "" {
		request.QueryParams["ExternalId"] = r.ExternalId
	}
	request.QueryParams["RoleSessionName"] = r.RoleSessionName
	request.QueryParams["SignatureMethod"] = "HMAC-SHA1"
	request.QueryParams["SignatureVersion"] = "1.0"
	request.QueryParams["Version"] = "2015-04-01"
	request.QueryParams["Timestamp"] = utils.GetTimeInFormatISO8601()
	request.QueryParams["SignatureNonce"] = utils.GetUUID()
	signature := utils.ShaHmac1(request.BuildStringToSign(), r.AccessKeySecret+"&")
	request.QueryParams["Signature"] = signature
	request.Headers["Host"] = request.Domain
	request.Headers["Accept-Encoding"] = "identity"
	request.URL = request.BuildURL()
	content, err := doAction(request, r.runtime)
	if err != nil {
		return fmt.Errorf("refresh RoleArn sts token err: %s", err.Error())
	}
	var resp *ramRoleArnResponse
	err = json.Unmarshal(content, &resp)
	if err != nil {
		return fmt.Errorf("refresh RoleArn sts token err: Json.Unmarshal fail: %s", err.Error())
	}
	if resp == nil || resp.Credentials == nil {
		return fmt.Errorf("refresh RoleArn sts token err: Credentials is empty")
	}
	respCredentials := resp.Credentials
	if respCredentials.AccessKeyId == "" || respCredentials.AccessKeySecret == "" || respCredentials.SecurityToken == "" || respCredentials.Expiration == "" {
		return fmt.Errorf("refresh RoleArn sts token err: AccessKeyId: %s, AccessKeySecret: %s, SecurityToken: %s, Expiration: %s", respCredentials.AccessKeyId, respCredentials.AccessKeySecret, respCredentials.SecurityToken, respCredentials.Expiration)
	}

	expirationTime, err := time.Parse("2006-01-02T15:04:05Z", respCredentials.Expiration)
	r.lastUpdateTimestamp = time.Now().Unix()
	r.credentialExpiration = int(expirationTime.Unix() - time.Now().Unix())
	r.sessionCredential = &sessionCredential{
		AccessKeyId:     respCredentials.AccessKeyId,
		AccessKeySecret: respCredentials.AccessKeySecret,
		SecurityToken:   respCredentials.SecurityToken,
	}

	return
}
