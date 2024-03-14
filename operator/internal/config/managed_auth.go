package config

import "os"

type AWSEnvironment struct {
	RoleARN string
}

type AzureEnvironment struct {
	ClientID       string
	SubscriptionID string
	TenantID       string
	Region         string
}

type TokenCCOAuthConfig struct {
	AWS   *AWSEnvironment
	Azure *AzureEnvironment
}

func discoverTokenCCOAuthConfig() *TokenCCOAuthConfig {
	// AWS
	roleARN := os.Getenv("ROLEARN")

	// Azure
	clientID := os.Getenv("CLIENTID")
	tenantID := os.Getenv("TENANTID")
	subscriptionID := os.Getenv("SUBSCRIPTIONID")
	region := os.Getenv("REGION")

	switch {
	case roleARN != "":
		return &TokenCCOAuthConfig{
			AWS: &AWSEnvironment{
				RoleARN: roleARN,
			},
		}
	case clientID != "" && tenantID != "" && subscriptionID != "":
		return &TokenCCOAuthConfig{
			Azure: &AzureEnvironment{
				ClientID:       clientID,
				SubscriptionID: subscriptionID,
				TenantID:       tenantID,
				Region:         region,
			},
		}
	}

	return nil
}
