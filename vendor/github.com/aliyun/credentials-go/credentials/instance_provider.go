package credentials

import (
	"os"
	"strings"

	"github.com/alibabacloud-go/tea/tea"
)

type instanceCredentialsProvider struct{}

var providerInstance = new(instanceCredentialsProvider)

func newInstanceCredentialsProvider() Provider {
	return &instanceCredentialsProvider{}
}

func (p *instanceCredentialsProvider) resolve() (*Config, error) {
	roleName, ok := os.LookupEnv(ENVEcsMetadata)
	if !ok {
		return nil, nil
	}
	enableIMDSv2, _ := os.LookupEnv(ENVEcsMetadataIMDSv2Enable)

	config := &Config{
		Type:         tea.String("ecs_ram_role"),
		RoleName:     tea.String(roleName),
		EnableIMDSv2: tea.Bool(strings.ToLower(enableIMDSv2) == "true"),
	}
	return config, nil
}
