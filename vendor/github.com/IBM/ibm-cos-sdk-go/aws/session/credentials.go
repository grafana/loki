package session

import (
	"fmt"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/processcreds"
	"github.com/IBM/ibm-cos-sdk-go/aws/request"
)

func resolveCredentials(cfg *aws.Config,
	envCfg envConfig, sharedCfg sharedConfig,
	handlers request.Handlers,
	sessOpts Options,
) (*credentials.Credentials, error) {

	switch {
	case len(sessOpts.Profile) != 0:
		// User explicitly provided a Profile in the session's configuration
		// so load that profile from shared config first.
		// Github(aws/aws-sdk-go#2727)
		return resolveCredsFromProfile(cfg, envCfg, sharedCfg, handlers, sessOpts)

	case envCfg.Creds.HasKeys():
		// Environment credentials
		return credentials.NewStaticCredentialsFromCreds(envCfg.Creds), nil

	default:
		// Fallback to the "default" credential resolution chain.
		return resolveCredsFromProfile(cfg, envCfg, sharedCfg, handlers, sessOpts)
	}
}

func resolveCredsFromProfile(cfg *aws.Config,
	envCfg envConfig, sharedCfg sharedConfig,
	handlers request.Handlers,
	sessOpts Options,
) (creds *credentials.Credentials, err error) {

	switch {
	case sharedCfg.SourceProfile != nil:
		// Assume IAM role with credentials source from a different profile.
		creds, err = resolveCredsFromProfile(cfg, envCfg,
			*sharedCfg.SourceProfile, handlers, sessOpts,
		)

	case sharedCfg.Creds.HasKeys():
		// Static Credentials from Shared Config/Credentials file.
		creds = credentials.NewStaticCredentialsFromCreds(
			sharedCfg.Creds,
		)

	case len(sharedCfg.CredentialProcess) != 0:
		// Get credentials from CredentialProcess
		creds = processcreds.NewCredentials(sharedCfg.CredentialProcess)

	case len(sharedCfg.CredentialSource) != 0:
		creds, err = resolveCredsFromSource(cfg, envCfg,
			sharedCfg, handlers, sessOpts,
		)

	default:
		// Fallback to default credentials provider, include mock errors for
		// the credential chain so user can identify why credentials failed to
		// be retrieved.
		creds = credentials.NewCredentials(&credentials.ChainProvider{
			VerboseErrors: aws.BoolValue(cfg.CredentialsChainVerboseErrors),
			Providers: []credentials.Provider{
				&credProviderError{
					Err: awserr.New("EnvAccessKeyNotFound",
						"failed to find credentials in the environment.", nil),
				},
				&credProviderError{
					Err: awserr.New("SharedCredsLoad",
						fmt.Sprintf("failed to load profile, %s.", envCfg.Profile), nil),
				},
				//defaults.RemoteCredProvider(*cfg, handlers), IBM
			},
		})
	}
	if err != nil {
		return nil, err
	}

	return creds, nil
}

// valid credential source values
const (
	credSourceEnvironment = "Environment"
)

func resolveCredsFromSource(cfg *aws.Config,
	envCfg envConfig, sharedCfg sharedConfig,
	handlers request.Handlers,
	sessOpts Options,
) (creds *credentials.Credentials, err error) {

	switch sharedCfg.CredentialSource {

	case credSourceEnvironment:
		creds = credentials.NewStaticCredentialsFromCreds(envCfg.Creds)

	default:
		return nil, ErrSharedConfigInvalidCredSource
	}

	return creds, nil
}

type credProviderError struct {
	Err error
}

func (c credProviderError) Retrieve() (credentials.Value, error) {
	return credentials.Value{}, c.Err
}
func (c credProviderError) IsExpired() bool {
	return true
}
