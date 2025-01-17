package config

import (
	"fmt"
	"os"
)

type AWSEnvironment struct {
	RoleARN string
}

type AzureEnvironment struct {
	ClientID       string
	SubscriptionID string
	TenantID       string
	Region         string
}

type GCPEnvironment struct {
	Audience            string
	ServiceAccountEmail string
}

type TokenCCOAuthConfig struct {
	AWS   *AWSEnvironment
	Azure *AzureEnvironment
	GCP   *GCPEnvironment
}

func discoverTokenCCOAuthConfig() *TokenCCOAuthConfig {
	// AWS
	roleARN := os.Getenv("ROLEARN")

	// Azure
	clientID := os.Getenv("CLIENTID")
	tenantID := os.Getenv("TENANTID")
	subscriptionID := os.Getenv("SUBSCRIPTIONID")
	region := os.Getenv("REGION")

	// GCP
	projectNumber := os.Getenv("PROJECT_NUMBER")
	poolID := os.Getenv("POOL_ID")
	providerID := os.Getenv("PROVIDER_ID")
	serviceAccountEmail := os.Getenv("SERVICE_ACCOUNT_EMAIL")

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
	case projectNumber != "" && poolID != "" && providerID != "" && serviceAccountEmail != "":
		audience := fmt.Sprintf(
			"//iam.googleapis.com/projects/%s/locations/global/workloadIdentityPools/%s/providers/%s",
			projectNumber,
			poolID,
			providerID,
		)

		return &TokenCCOAuthConfig{
			GCP: &GCPEnvironment{
				Audience:            audience,
				ServiceAccountEmail: serviceAccountEmail,
			},
		}
	}

	return nil
}
