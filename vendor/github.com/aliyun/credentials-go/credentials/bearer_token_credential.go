package credentials

import "github.com/alibabacloud-go/tea/tea"

// BearerTokenCredential is a kind of credential
type BearerTokenCredential struct {
	BearerToken string
}

// newBearerTokenCredential return a BearerTokenCredential object
func newBearerTokenCredential(token string) *BearerTokenCredential {
	return &BearerTokenCredential{
		BearerToken: token,
	}
}

func (s *BearerTokenCredential) GetCredential() (*CredentialModel, error) {
	credential := &CredentialModel{
		BearerToken:  tea.String(s.BearerToken),
		Type:         tea.String("bearer"),
		ProviderName: tea.String("bearer"),
	}
	return credential, nil
}

// GetAccessKeyId is useless for BearerTokenCredential
func (b *BearerTokenCredential) GetAccessKeyId() (*string, error) {
	return tea.String(""), nil
}

// GetAccessSecret is useless for BearerTokenCredential
func (b *BearerTokenCredential) GetAccessKeySecret() (*string, error) {
	return tea.String(("")), nil
}

// GetSecurityToken is useless for BearerTokenCredential
func (b *BearerTokenCredential) GetSecurityToken() (*string, error) {
	return tea.String(""), nil
}

// GetBearerToken reutrns  BearerTokenCredential's BearerToken
func (b *BearerTokenCredential) GetBearerToken() *string {
	return tea.String(b.BearerToken)
}

// GetType reutrns  BearerTokenCredential's type
func (b *BearerTokenCredential) GetType() *string {
	return tea.String("bearer")
}
