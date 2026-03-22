// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package s3

import (
	"context"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/pkg/errors"
)

// AWSSDKAuth retrieves credentials from the aws-sdk-go.
type AWSSDKAuth struct {
	Region string
	creds  aws.Credentials
}

// NewAWSSDKAuth returns a pointer to a new Credentials object
// wrapping the environment variable provider.
func NewAWSSDKAuth(region string) *credentials.Credentials {
	return credentials.New(&AWSSDKAuth{
		Region: region,
	})
}

func (a *AWSSDKAuth) RetrieveWithCredContext(cc *credentials.CredContext) (credentials.Value, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.TODO(), awsconfig.WithRegion(a.Region))
	if err != nil {
		return credentials.Value{}, errors.Wrap(err, "load AWS SDK config")
	}

	creds, err := cfg.Credentials.Retrieve(context.TODO())
	if err != nil {
		return credentials.Value{}, errors.Wrap(err, "retrieve AWS SDK credentials")
	}

	a.creds = creds

	return credentials.Value{
		AccessKeyID:     creds.AccessKeyID,
		SecretAccessKey: creds.SecretAccessKey,
		SessionToken:    creds.SessionToken,
		SignerType:      credentials.SignatureV4,
	}, nil
}

// Retrieve retrieves the keys from the environment.
func (a *AWSSDKAuth) Retrieve() (credentials.Value, error) {
	return a.RetrieveWithCredContext(nil)
}

// IsExpired returns if the credentials have been retrieved.
func (a *AWSSDKAuth) IsExpired() bool {
	return a.creds.Expired()
}
