package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/smithy-go/ptr"
	"os"
)

func loadEnv(ctx context.Context, name string) (string, error) {
	envValue, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %s not found", name)
	}

	if arn.IsARN(envValue) {
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			return "", fmt.Errorf("error loading aws config: %w", err)
		}

		parsedArn, err := arn.Parse(envValue)
		if err != nil {
			return "", fmt.Errorf("error parsing arn: %w", err)
		}

		switch parsedArn.Service {
		case "secretsmanager":
			client := secretsmanager.NewFromConfig(cfg)
			out, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
				SecretId: &envValue,
			})
			if err != nil {
				return "", fmt.Errorf("error fetching secret %s: %w", envValue, err)
			}

			return *out.SecretString, nil
		case "ssm":
			client := ssm.NewFromConfig(cfg)
			out, err := client.GetParameter(ctx, &ssm.GetParameterInput{
				Name:           &envValue,
				WithDecryption: ptr.Bool(true),
			})
			if err != nil {
				return "", fmt.Errorf("error fetching parameter %s: %w", envValue, err)
			}

			return *out.Parameter.Value, nil
		default:
			return "", fmt.Errorf("environment variable %s set to an invalid ARN (unsupported service %s)", name, parsedArn.Service)
		}
	}

	return envValue, nil
}
