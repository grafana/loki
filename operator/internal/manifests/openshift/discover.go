package openshift

import (
	"os"
)

type AWSSTSEnv struct {
	RoleARN string
}

type ManagedAuthEnv struct {
	AWS *AWSSTSEnv
}

func DiscoverManagedAuthEnv() *ManagedAuthEnv {
	// AWS
	roleARN := os.Getenv("ROLEARN")

	switch {
	case roleARN != "":
		return &ManagedAuthEnv{
			AWS: &AWSSTSEnv{
				RoleARN: roleARN,
			},
		}
	}

	return nil
}
