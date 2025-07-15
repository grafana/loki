package core

// (C) Copyright IBM Corp. 2019.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
)

const (
	// IBM_CREDENTIAL_FILE_ENVVAR is the environment key used to find the path to
	// a credentials file.
	IBM_CREDENTIAL_FILE_ENVVAR = "IBM_CREDENTIALS_FILE"

	// DEFAULT_CREDENTIAL_FILE_NAME is the default filename for a credentials file.
	// It is used when "IBM_CREDENTIALS_FILE" is not specified. The filename will
	// be searched for within the program's working directory, and then the OS's
	// current user directory.
	DEFAULT_CREDENTIAL_FILE_NAME = "ibm-credentials.env"
)

// GetServiceProperties returns a map containing configuration properties for the specified service
// that are retrieved from external configuration sources in the following precedence order:
// 1) credential file
// 2) environment variables
// 3) VCAP_SERVICES
//
// 'serviceName' is used as a filter against the property names.  For example, if serviceName is
// passed in as "my_service", then configuration properties whose names begin with "MY_SERVICE_"
// will be returned in the map.
func GetServiceProperties(serviceName string) (serviceProps map[string]string, err error) {
	serviceProps, err = getServiceProperties(serviceName)
	err = RepurposeSDKProblem(err, "get-props-error")
	return
}

// getServiceProperties: This function will retrieve configuration properties for the specified service
// from external config sources in the following precedence order:
// 1) credential file
// 2) environment variables
// 3) VCAP_SERVICES
func getServiceProperties(serviceName string) (serviceProps map[string]string, err error) {

	if serviceName == "" {
		err = fmt.Errorf("serviceName was not specified")
		err = SDKErrorf(err, "", "no-service-name", getComponentInfo())
		return
	}

	GetLogger().Debug("Retrieving config properties for service '%s'\n", serviceName)

	// First try to retrieve service properties from a credential file.
	serviceProps = getServicePropertiesFromCredentialFile(serviceName)

	// Next, try to retrieve them from environment variables.
	if serviceProps == nil {
		serviceProps = getServicePropertiesFromEnvironment(serviceName)
	}

	// Finally, try to retrieve them from VCAP_SERVICES.
	if serviceProps == nil {
		serviceProps = getServicePropertiesFromVCAP(serviceName)
	}

	GetLogger().Debug("Retrieved %d properties\n", len(serviceProps))

	return
}

// getServicePropertiesFromCredentialFile: returns a map containing properties found within a credential file
// that are associated with the specified credentialKey.  Returns a nil map if no properties are found.
// Credential file search order:
// 1) ${IBM_CREDENTIALS_FILE}
// 2) <user-home-dir>/ibm-credentials.env
// 3) <current-working-directory>/ibm-credentials.env
func getServicePropertiesFromCredentialFile(credentialKey string) map[string]string {

	// Check the search order for the credential file that we'll attempt to load:
	var credentialFilePath string

	// 1) ${IBM_CREDENTIALS_FILE}
	envPath := os.Getenv(IBM_CREDENTIAL_FILE_ENVVAR)
	if _, err := os.Stat(envPath); err == nil {
		credentialFilePath = envPath
	}

	// 2) <current-working-directory>/ibm-credentials.env
	if credentialFilePath == "" {
		dir, _ := os.Getwd()
		var filePath = path.Join(dir, DEFAULT_CREDENTIAL_FILE_NAME)
		if _, err := os.Stat(filePath); err == nil {
			credentialFilePath = filePath
		}
	}

	// 3) <user-home-dir>/ibm-credentials.env
	if credentialFilePath == "" {
		var filePath = path.Join(UserHomeDir(), DEFAULT_CREDENTIAL_FILE_NAME)
		if _, err := os.Stat(filePath); err == nil {
			credentialFilePath = filePath
		}
	}

	// If we found a file to load, then load it.
	if credentialFilePath != "" {
		file, err := os.Open(credentialFilePath) // #nosec G304
		if err != nil {
			return nil
		}
		defer file.Close() // #nosec G307

		// Collect the contents of the credential file in a string array.
		lines := make([]string, 0)
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}

		// Parse the file contents into name/value pairs.
		return parsePropertyStrings(credentialKey, lines)
	}

	return nil
}

// getServicePropertiesFromEnvironment: returns a map containing properties found within the environment
// that are associated with the specified credentialKey.  Returns a nil map if no properties are found.
func getServicePropertiesFromEnvironment(credentialKey string) map[string]string {
	return parsePropertyStrings(credentialKey, os.Environ())
}

// getServicePropertiesFromVCAP: returns a map containing properties found within the VCAP_SERVICES
// environment variable for the specified credentialKey (service name). Returns a nil map if no properties are found.
func getServicePropertiesFromVCAP(credentialKey string) map[string]string {
	credentials := loadFromVCAPServices(credentialKey)
	if credentials != nil {
		props := make(map[string]string)
		if credentials.URL != "" {
			props[PROPNAME_SVC_URL] = credentials.URL
		}

		if credentials.Username != "" {
			props[PROPNAME_USERNAME] = credentials.Username
		}

		if credentials.Password != "" {
			props[PROPNAME_PASSWORD] = credentials.Password
		}

		if credentials.APIKey != "" {
			props[PROPNAME_APIKEY] = credentials.APIKey
		}

		// If no values were actually found in this credential entry, then bail out now.
		if len(props) == 0 {
			return nil
		}

		// Make a (hopefully good) guess at the auth type.
		authType := ""
		if props[PROPNAME_APIKEY] != "" {
			authType = AUTHTYPE_IAM
		} else if props[PROPNAME_USERNAME] != "" || props[PROPNAME_PASSWORD] != "" {
			authType = AUTHTYPE_BASIC
		} else {
			authType = AUTHTYPE_IAM
		}
		props[PROPNAME_AUTH_TYPE] = authType

		return props
	}

	return nil
}

// parsePropertyStrings: accepts an array of strings of the form "<key>=<value>" and parses/filters them to
// produce a map of properties associated with the specified credentialKey.
func parsePropertyStrings(credentialKey string, propertyStrings []string) map[string]string {
	if len(propertyStrings) == 0 {
		return nil
	}

	props := make(map[string]string)
	credentialKey = strings.ToUpper(credentialKey)
	credentialKey = strings.Replace(credentialKey, "-", "_", -1)
	credentialKey += "_"
	for _, propertyString := range propertyStrings {

		// Trim the property string and ignore any blank or comment lines.
		propertyString = strings.TrimSpace(propertyString)
		if propertyString == "" || strings.HasPrefix(propertyString, "#") {
			continue
		}

		// Parse the property string into name and value tokens
		var tokens = strings.SplitN(propertyString, "=", 2)
		if len(tokens) == 2 {
			// Does the name start with the credential key?
			// If so, then extract the property name by filtering out the credential key,
			// then store the name/value pair in the map.
			if strings.HasPrefix(tokens[0], credentialKey) && (len(tokens[0]) > len(credentialKey)) {
				name := tokens[0][len(credentialKey):]
				value := strings.TrimSpace(tokens[1])
				props[name] = value
			}
		}
	}

	if len(props) == 0 {
		return nil
	}
	return props
}

// Service : The service
type service struct {
	Name        string      `json:"name,omitempty"`
	Credentials *credential `json:"credentials,omitempty"`
}

// Credential : The service credential
type credential struct {
	URL      string `json:"url,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	APIKey   string `json:"apikey,omitempty"`
}

// LoadFromVCAPServices : returns the credential of the service
func loadFromVCAPServices(serviceName string) *credential {
	vcapServices := os.Getenv("VCAP_SERVICES")
	if vcapServices != "" {
		var rawServices map[string][]service
		if err := json.Unmarshal([]byte(vcapServices), &rawServices); err != nil {
			return nil
		}
		for _, serviceEntries := range rawServices {
			for _, service := range serviceEntries {
				if service.Name == serviceName {
					return service.Credentials
				}
			}
		}
		if serviceList, exists := rawServices[serviceName]; exists && len(serviceList) > 0 {
			return serviceList[0].Credentials
		}
	}
	return nil
}
