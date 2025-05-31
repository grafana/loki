package main

import (
	"context"
	"github.com/stretchr/testify/assert"
	"testing"
)

func Test_loadSensitiveEnv(t *testing.T) {
	ctx := context.Background()

	t.Run("should return env variable if set", func(t *testing.T) {
		t.Setenv("FOO", "BAR")
		secretsClient := &testSecretsClient{}

		value, err := loadSensitiveEnv(ctx, secretsClient, "FOO")

		assert.NoError(t, err)
		assert.Equal(t, "BAR", value)
		assert.Equal(t, 0, secretsClient.CallsFetchFromAWSSecretsManager)
		assert.Equal(t, 0, secretsClient.CallsFetchFromAWSSSMParameterStore)
	})

	t.Run("should not return an error if env is not set", func(t *testing.T) {
		secretsClient := &testSecretsClient{}

		value, err := loadSensitiveEnv(ctx, secretsClient, "FOO")

		assert.NoError(t, err)
		assert.Empty(t, value)
		assert.Equal(t, 0, secretsClient.CallsFetchFromAWSSecretsManager)
		assert.Equal(t, 0, secretsClient.CallsFetchFromAWSSSMParameterStore)
	})

	t.Run("should return an error if the env variable contains an invalid arn", func(t *testing.T) {
		t.Setenv("FOO", "arn:aws:invalid:eu-west-1:123456789012:ssm/example")
		secretsClient := &testSecretsClient{}

		value, err := loadSensitiveEnv(ctx, secretsClient, "FOO")

		assert.Error(t, err)
		assert.Empty(t, value)
		assert.Equal(t, 0, secretsClient.CallsFetchFromAWSSecretsManager)
		assert.Equal(t, 0, secretsClient.CallsFetchFromAWSSSMParameterStore)
	})

	t.Run("should call FetchFromAWSSecretsManager if the env variable contains a secret ARN", func(t *testing.T) {
		t.Setenv("FOO", "arn:aws:secretsmanager:eu-west-1:123456789012:secret:foo")
		secretsClient := &testSecretsClient{
			ExpectedArn: "arn:aws:secretsmanager:eu-west-1:123456789012:secret:foo",
			ReturnValue: "bar",
		}

		value, err := loadSensitiveEnv(ctx, secretsClient, "FOO")
		assert.NoError(t, err)
		assert.Equal(t, "bar", value)
		assert.Equal(t, 1, secretsClient.CallsFetchFromAWSSecretsManager)
		assert.Equal(t, 0, secretsClient.CallsFetchFromAWSSSMParameterStore)
	})

	t.Run("should call FetchFromAWSSSMParameterStore if the env variable contains a parameter ARN", func(t *testing.T) {
		t.Setenv("FOO", "arn:aws:ssm:eu-west-1:123456789012:parameter/foo")
		secretsClient := &testSecretsClient{
			ExpectedArn: "arn:aws:ssm:eu-west-1:123456789012:parameter/foo",
			ReturnValue: "bar",
		}

		value, err := loadSensitiveEnv(ctx, secretsClient, "FOO")
		assert.NoError(t, err)
		assert.Equal(t, "bar", value)
		assert.Equal(t, 0, secretsClient.CallsFetchFromAWSSecretsManager)
		assert.Equal(t, 1, secretsClient.CallsFetchFromAWSSSMParameterStore)
	})
}
