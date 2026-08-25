package providers

// 下一版本 Credentials 包
// - 分离 bearer token
// - 从 config 传递迁移到真正的 credentials provider 模式
// - 删除 GetAccessKeyId()/GetAccessKeySecret()/GetSecurityToken() 方法，只保留 GetCredentials()

// The credentials struct
type Credentials struct {
	AccessKeyId     string
	AccessKeySecret string
	SecurityToken   string
	ProviderName    string
}

// The credentials provider interface, return credentials and provider name
type CredentialsProvider interface {
	// Get credentials
	GetCredentials() (*Credentials, error)
	// Get credentials provider name
	GetProviderName() string
}
