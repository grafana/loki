package credentials

import "github.com/alibabacloud-go/tea/tea"

// CredentialModel is a model
type CredentialModel struct {
	// accesskey id
	AccessKeyId *string `json:"accessKeyId,omitempty" xml:"accessKeyId,omitempty"`
	// accesskey secret
	AccessKeySecret *string `json:"accessKeySecret,omitempty" xml:"accessKeySecret,omitempty"`
	// security token
	SecurityToken *string `json:"securityToken,omitempty" xml:"securityToken,omitempty"`
	// bearer token
	BearerToken *string `json:"bearerToken,omitempty" xml:"bearerToken,omitempty"`
	// type
	//
	// example:
	//
	// access_key
	Type *string `json:"type,omitempty" xml:"type,omitempty"`
	// provider name
	//
	// example:
	//
	// cli_profile/static_ak
	ProviderName *string `json:"providerName,omitempty" xml:"providerName,omitempty"`
}

func (s CredentialModel) String() string {
	return tea.Prettify(s)
}

func (s CredentialModel) GoString() string {
	return s.String()
}

func (s *CredentialModel) SetAccessKeyId(v string) *CredentialModel {
	s.AccessKeyId = &v
	return s
}

func (s *CredentialModel) SetAccessKeySecret(v string) *CredentialModel {
	s.AccessKeySecret = &v
	return s
}

func (s *CredentialModel) SetSecurityToken(v string) *CredentialModel {
	s.SecurityToken = &v
	return s
}

func (s *CredentialModel) SetBearerToken(v string) *CredentialModel {
	s.BearerToken = &v
	return s
}

func (s *CredentialModel) SetType(v string) *CredentialModel {
	s.Type = &v
	return s
}

func (s *CredentialModel) SetProviderName(v string) *CredentialModel {
	s.ProviderName = &v
	return s
}
