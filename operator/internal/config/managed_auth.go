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

type ManagedAuthConfig struct {
	AWS   *AWSEnvironment
	Azure *AzureEnvironment
}

func discoverManagedAuthConfig() *ManagedAuthConfig {
	// AWS
	roleARN := os.Getenv("ROLEARN")

	// Azure
	clientID := os.Getenv("CLIENTID")
	tenantID := os.Getenv("TENANTID")
	subscriptionID := os.Getenv("SUBSCRIPTIONID")

	switch {
	case roleARN != "":
		return &ManagedAuthConfig{
			AWS: &AWSEnvironment{
				RoleARN: roleARN,
			},
		}
	case clientID != "" && tenantID != "" && subscriptionID != "":
		return &ManagedAuthConfig{
			Azure: &AzureEnvironment{
				ClientID:       clientID,
				SubscriptionID: subscriptionID,
				TenantID:       tenantID,
			},
		}
	}

	return nil
}
