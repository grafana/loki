package ibmiam

import (
	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/internal/ini"
)

const (
	// Default Profile
	defaultProfile = "default"
)

// commonIni constructor of the IBM IAM provider that loads IAM credentials from
// an ini file
// Parameters:
//		AWS Config
// 		Profile filename
//		Profile prefix
// Returns:
//		New provider with Provider name, config, API Key, IBM IAM Authentication Server end point,
//		Service Instance ID
func commonIniProvider(providerName string, config *aws.Config, filename, profilename string) *Provider {

	// Opens an ini file with the filename passed in for shared credentials
	// If fails, returns error
	ini, err := ini.OpenFile(filename)
	if err != nil {
		e := awserr.New("SharedCredentialsOpenError", "Shared Credentials Open Error", err)
		logFromConfigHelper(config, "<DEBUG>", "<IBM IAM PROVIDER BUILD>", providerName, e)
		return &Provider{
			providerName: SharedConfProviderName,
			ErrorStatus:  e,
		}
	}

	// Gets section of the shared credentials ini file
	// If fails, returns error
	iniProfile, ok := ini.GetSection(profilename)
	if !ok {
		e := awserr.New("SharedCredentialsProfileNotFound",
			"Shared Credentials Section '"+profilename+"' not Found in file '"+filename+"'", nil)
		logFromConfigHelper(config, "<DEBUG>", "<IBM IAM PROVIDER BUILD>", providerName, e)
		return &Provider{
			providerName: SharedConfProviderName,
			ErrorStatus:  e,
		}
	}

	// Populaute the IBM IAM Credential values
	apiKey := iniProfile.String("ibm_api_key_id")
	serviceInstanceID := iniProfile.String("ibm_service_instance_id")
	authEndPoint := iniProfile.String("ibm_auth_endpoint")

	return NewProvider(providerName, config, apiKey, authEndPoint, serviceInstanceID, nil)
}

// Log From Config
func logFromConfigHelper(config *aws.Config, params ...interface{}) {
	logLevel := aws.LogLevel(aws.LogOff)
	if config != nil && config.LogLevel != nil && config.Logger != nil {
		logLevel = config.LogLevel
	}
	if logLevel.Matches(aws.LogDebug) {
		config.Logger.Log(params)
	}
}
