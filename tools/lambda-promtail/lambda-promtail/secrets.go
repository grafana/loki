package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/smithy-go/ptr"
)

var _ secretFetcher = &secretClients{}

type secretClients struct{}

func (c *secretClients) FetchFromAWSSecretsManager(ctx context.Context, secretArn string) (string, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("error loading aws config: %w", err)
	}

	client := secretsmanager.NewFromConfig(cfg)
	out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: &secretArn,
	})
	if err != nil {
		return "", fmt.Errorf("error fetching secret %s: %w", secretArn, err)
	}

	return *out.SecretString, nil
}

func (c *secretClients) FetchFromAWSSSMParameterStore(ctx context.Context, parameterArn string) (string, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("error loading aws config: %w", err)
	}

	client := ssm.NewFromConfig(cfg)
	out, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           &parameterArn,
		WithDecryption: ptr.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("error fetching SSM parameter %s: %w", parameterArn, err)
	}

	return *out.Parameter.Value, nil
}
