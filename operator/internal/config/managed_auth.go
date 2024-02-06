package config

import "os"

type AWSSTSEnv struct {
	RoleARN string
}

type AzureWIFEnvironment struct {
	ClientID       string
	SubscriptionID string
	TenantID       string
	Region         string
}

type ManagedAuthEnv struct {
	AWS   *AWSSTSEnv
	Azure *AzureWIFEnvironment
}

func discoverManagedAuthEnv() *ManagedAuthEnv {
	// AWS
	roleARN := os.Getenv("ROLEARN")

	// Azure
	clientID := os.Getenv("CLIENTID")
	tenantID := os.Getenv("TENANTID")
	subscriptionID := os.Getenv("SUBSCRIPTIONID")

	switch {
	case roleARN != "":
		return &ManagedAuthEnv{
			AWS: &AWSSTSEnv{
				RoleARN: roleARN,
			},
		}
	case clientID != "" && tenantID != "" && subscriptionID != "":
		return &ManagedAuthEnv{
			Azure: &AzureWIFEnvironment{
				ClientID:       clientID,
				SubscriptionID: subscriptionID,
				TenantID:       tenantID,
			},
		}
	}

	return nil
}
