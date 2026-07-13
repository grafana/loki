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

// Deprecated: no more recommend to use it
// RsaKeyPairCredentialsProvider is a kind of credentials provider
type RsaKeyPairCredentialsProvider struct {
	*credentialUpdater
	PrivateKey        string
	PublicKeyId       string
	SessionExpiration int
	sessionCredential *sessionCredential
	runtime           *utils.Runtime
}

type rsaKeyPairResponse struct {
	SessionAccessKey *sessionAccessKey `json:"SessionAccessKey" xml:"SessionAccessKey"`
}

type sessionAccessKey struct {
	SessionAccessKeyId     string `json:"SessionAccessKeyId" xml:"SessionAccessKeyId"`
	SessionAccessKeySecret string `json:"SessionAccessKeySecret" xml:"SessionAccessKeySecret"`
	Expiration             string `json:"Expiration" xml:"Expiration"`
}

func newRsaKeyPairCredential(privateKey, publicKeyId string, sessionExpiration int, runtime *utils.Runtime) *RsaKeyPairCredentialsProvider {
	return &RsaKeyPairCredentialsProvider{
		PrivateKey:        privateKey,
		PublicKeyId:       publicKeyId,
		SessionExpiration: sessionExpiration,
		credentialUpdater: new(credentialUpdater),
		runtime:           runtime,
	}
}

func (e *RsaKeyPairCredentialsProvider) GetCredential() (*CredentialModel, error) {
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
		Type:            tea.String("rsa_key_pair"),
	}
	return credential, nil
}

// GetAccessKeyId reutrns RsaKeyPairCredential's AccessKeyId
// if AccessKeyId is not exist or out of date, the function will update it.
func (r *RsaKeyPairCredentialsProvider) GetAccessKeyId() (accessKeyId *string, err error) {
	c, err := r.GetCredential()
	if err != nil {
		return
	}
	accessKeyId = c.AccessKeyId
	return
}

// GetAccessSecret reutrns  RsaKeyPairCredential's AccessKeySecret
// if AccessKeySecret is not exist or out of date, the function will update it.
func (r *RsaKeyPairCredentialsProvider) GetAccessKeySecret() (accessKeySecret *string, err error) {
	c, err := r.GetCredential()
	if err != nil {
		return
	}
	accessKeySecret = c.AccessKeySecret
	return
}

// GetSecurityToken is useless  RsaKeyPairCredential
func (r *RsaKeyPairCredentialsProvider) GetSecurityToken() (*string, error) {
	return tea.String(""), nil
}

// GetBearerToken is useless for  RsaKeyPairCredential
func (r *RsaKeyPairCredentialsProvider) GetBearerToken() *string {
	return tea.String("")
}

// GetType reutrns  RsaKeyPairCredential's type
func (r *RsaKeyPairCredentialsProvider) GetType() *string {
	return tea.String("rsa_key_pair")
}

func (r *RsaKeyPairCredentialsProvider) updateCredential() (err error) {
	if r.runtime == nil {
		r.runtime = new(utils.Runtime)
	}
	request := request.NewCommonRequest()
	request.Domain = "sts.aliyuncs.com"
	if r.runtime.Host != "" {
		request.Domain = r.runtime.Host
	} else if r.runtime.STSEndpoint != "" {
		request.Domain = r.runtime.STSEndpoint
	}
	request.Scheme = "HTTPS"
	request.Method = "GET"
	request.QueryParams["AccessKeyId"] = r.PublicKeyId
	request.QueryParams["Action"] = "GenerateSessionAccessKey"
	request.QueryParams["Format"] = "JSON"
	if r.SessionExpiration > 0 {
		if r.SessionExpiration >= 900 && r.SessionExpiration <= 3600 {
			request.QueryParams["DurationSeconds"] = strconv.Itoa(r.SessionExpiration)
		} else {
			err = errors.New("[InvalidParam]:Key Pair session duration should be in the range of 15min - 1Hr")
			return
		}
	} else {
		request.QueryParams["DurationSeconds"] = strconv.Itoa(defaultDurationSeconds)
	}
	request.QueryParams["SignatureMethod"] = "SHA256withRSA"
	request.QueryParams["SignatureType"] = "PRIVATEKEY"
	request.QueryParams["SignatureVersion"] = "1.0"
	request.QueryParams["Version"] = "2015-04-01"
	request.QueryParams["Timestamp"] = utils.GetTimeInFormatISO8601()
	request.QueryParams["SignatureNonce"] = utils.GetUUID()
	signature := utils.Sha256WithRsa(request.BuildStringToSign(), r.PrivateKey)
	request.QueryParams["Signature"] = signature
	request.Headers["Host"] = request.Domain
	request.Headers["Accept-Encoding"] = "identity"
	request.URL = request.BuildURL()
	content, err := doAction(request, r.runtime)
	if err != nil {
		return fmt.Errorf("refresh KeyPair err: %s", err.Error())
	}
	var resp *rsaKeyPairResponse
	err = json.Unmarshal(content, &resp)
	if err != nil {
		return fmt.Errorf("refresh KeyPair err: Json Unmarshal fail: %s", err.Error())
	}
	if resp == nil || resp.SessionAccessKey == nil {
		return fmt.Errorf("refresh KeyPair err: SessionAccessKey is empty")
	}
	sessionAccessKey := resp.SessionAccessKey
	if sessionAccessKey.SessionAccessKeyId == "" || sessionAccessKey.SessionAccessKeySecret == "" || sessionAccessKey.Expiration == "" {
		return fmt.Errorf("refresh KeyPair err: SessionAccessKeyId: %v, SessionAccessKeySecret: %v, Expiration: %v", sessionAccessKey.SessionAccessKeyId, sessionAccessKey.SessionAccessKeySecret, sessionAccessKey.Expiration)
	}

	expirationTime, err := time.Parse("2006-01-02T15:04:05Z", sessionAccessKey.Expiration)
	r.lastUpdateTimestamp = time.Now().Unix()
	r.credentialExpiration = int(expirationTime.Unix() - time.Now().Unix())
	r.sessionCredential = &sessionCredential{
		AccessKeyId:     sessionAccessKey.SessionAccessKeyId,
		AccessKeySecret: sessionAccessKey.SessionAccessKeySecret,
	}

	return
}
