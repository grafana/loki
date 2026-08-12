package credentials

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/credentials-go/credentials/internal/utils"
	"github.com/aliyun/credentials-go/credentials/request"
)

var securityCredURL = "http://100.100.100.200/latest/meta-data/ram/security-credentials/"
var securityCredTokenURL = "http://100.100.100.200/latest/api/token"

const defaultMetadataTokenDuration = int(21600)

// ECSRAMRoleCredentialsProvider is a kind of credentials provider
type ECSRAMRoleCredentialsProvider struct {
	*credentialUpdater
	RoleName              string
	EnableIMDSv2          bool
	MetadataTokenDuration int
	sessionCredential     *sessionCredential
	runtime               *utils.Runtime
	metadataToken         string
	staleTime             int64
}

type ecsRAMRoleResponse struct {
	Code            string `json:"Code" xml:"Code"`
	AccessKeyId     string `json:"AccessKeyId" xml:"AccessKeyId"`
	AccessKeySecret string `json:"AccessKeySecret" xml:"AccessKeySecret"`
	SecurityToken   string `json:"SecurityToken" xml:"SecurityToken"`
	Expiration      string `json:"Expiration" xml:"Expiration"`
}

func newEcsRAMRoleCredentialWithEnableIMDSv2(roleName string, enableIMDSv2 bool, metadataTokenDuration int, inAdvanceScale float64, runtime *utils.Runtime) *ECSRAMRoleCredentialsProvider {
	credentialUpdater := new(credentialUpdater)
	if inAdvanceScale < 1 && inAdvanceScale > 0 {
		credentialUpdater.inAdvanceScale = inAdvanceScale
	}
	return &ECSRAMRoleCredentialsProvider{
		RoleName:              roleName,
		EnableIMDSv2:          enableIMDSv2,
		MetadataTokenDuration: metadataTokenDuration,
		credentialUpdater:     credentialUpdater,
		runtime:               runtime,
	}
}

func (e *ECSRAMRoleCredentialsProvider) GetCredential() (credentials *CredentialModel, err error) {
	if e.sessionCredential == nil || e.needUpdateCredential() {
		err = e.updateCredential()
		if err != nil {
			if e.credentialExpiration > (int(time.Now().Unix()) - int(e.lastUpdateTimestamp)) {
				// 虽然有错误，但是已有的 credentials 还有效
			} else {
				return
			}
		}
	}

	credentials = &CredentialModel{
		AccessKeyId:     tea.String(e.sessionCredential.AccessKeyId),
		AccessKeySecret: tea.String(e.sessionCredential.AccessKeySecret),
		SecurityToken:   tea.String(e.sessionCredential.SecurityToken),
		Type:            tea.String("ecs_ram_role"),
	}

	return
}

// GetAccessKeyId reutrns  EcsRAMRoleCredential's AccessKeyId
// if AccessKeyId is not exist or out of date, the function will update it.
func (e *ECSRAMRoleCredentialsProvider) GetAccessKeyId() (accessKeyId *string, err error) {
	c, err := e.GetCredential()
	if err != nil {
		return
	}

	accessKeyId = c.AccessKeyId
	return
}

// GetAccessSecret reutrns  EcsRAMRoleCredential's AccessKeySecret
// if AccessKeySecret is not exist or out of date, the function will update it.
func (e *ECSRAMRoleCredentialsProvider) GetAccessKeySecret() (accessKeySecret *string, err error) {
	c, err := e.GetCredential()
	if err != nil {
		return
	}

	accessKeySecret = c.AccessKeySecret
	return
}

// GetSecurityToken reutrns  EcsRAMRoleCredential's SecurityToken
// if SecurityToken is not exist or out of date, the function will update it.
func (e *ECSRAMRoleCredentialsProvider) GetSecurityToken() (securityToken *string, err error) {
	c, err := e.GetCredential()
	if err != nil {
		return
	}

	securityToken = c.SecurityToken
	return
}

// GetBearerToken is useless for EcsRAMRoleCredential
func (e *ECSRAMRoleCredentialsProvider) GetBearerToken() *string {
	return tea.String("")
}

// GetType reutrns  EcsRAMRoleCredential's type
func (e *ECSRAMRoleCredentialsProvider) GetType() *string {
	return tea.String("ecs_ram_role")
}

func getRoleName() (string, error) {
	runtime := utils.NewRuntime(1, 1, "", "")
	request := request.NewCommonRequest()
	request.URL = securityCredURL
	request.Method = "GET"
	content, err := doAction(request, runtime)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func (e *ECSRAMRoleCredentialsProvider) getMetadataToken() (err error) {
	if e.needToRefresh() {
		if e.MetadataTokenDuration <= 0 {
			e.MetadataTokenDuration = defaultMetadataTokenDuration
		}
		tmpTime := time.Now().Unix() + int64(e.MetadataTokenDuration*1000)
		request := request.NewCommonRequest()
		request.URL = securityCredTokenURL
		request.Method = "PUT"
		request.Headers["X-aliyun-ecs-metadata-token-ttl-seconds"] = strconv.Itoa(e.MetadataTokenDuration)
		content, err := doAction(request, e.runtime)
		if err != nil {
			return err
		}
		e.staleTime = tmpTime
		e.metadataToken = string(content)
	}
	return
}

func (e *ECSRAMRoleCredentialsProvider) updateCredential() (err error) {
	if e.runtime == nil {
		e.runtime = new(utils.Runtime)
	}
	request := request.NewCommonRequest()
	if e.RoleName == "" {
		e.RoleName, err = getRoleName()
		if err != nil {
			return fmt.Errorf("refresh Ecs sts token err: %s", err.Error())
		}
	}
	if e.EnableIMDSv2 {
		err = e.getMetadataToken()
		if err != nil {
			return fmt.Errorf("failed to get token from ECS Metadata Service: %s", err.Error())
		}
		request.Headers["X-aliyun-ecs-metadata-token"] = e.metadataToken
	}
	request.URL = securityCredURL + e.RoleName
	request.Method = "GET"
	content, err := doAction(request, e.runtime)
	if err != nil {
		return fmt.Errorf("refresh Ecs sts token err: %s", err.Error())
	}
	var resp *ecsRAMRoleResponse
	err = json.Unmarshal(content, &resp)
	if err != nil {
		return fmt.Errorf("refresh Ecs sts token err: Json Unmarshal fail: %s", err.Error())
	}
	if resp.Code != "Success" {
		return fmt.Errorf("refresh Ecs sts token err: Code is not Success")
	}
	if resp.AccessKeyId == "" || resp.AccessKeySecret == "" || resp.SecurityToken == "" || resp.Expiration == "" {
		return fmt.Errorf("refresh Ecs sts token err: AccessKeyId: %s, AccessKeySecret: %s, SecurityToken: %s, Expiration: %s", resp.AccessKeyId, resp.AccessKeySecret, resp.SecurityToken, resp.Expiration)
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

func (e *ECSRAMRoleCredentialsProvider) needToRefresh() (needToRefresh bool) {
	needToRefresh = time.Now().Unix() >= e.staleTime
	return
}
