// Copyright (c) 2016, 2018, 2025, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.

package common

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Region type for regions
type Region string

const (
	instanceMetadataRegionInfoURLV2 = "http://169.254.169.254/opc/v2/instance/regionInfo"

	// Region Metadata Configuration File
	regionMetadataCfgDirName  = ".oci"
	regionMetadataCfgFileName = "regions-config.json"

	// Region Metadata Environment Variable
	regionMetadataEnvVarName = "OCI_REGION_METADATA"

	// Default Realm Environment Variable
	defaultRealmEnvVarName = "OCI_DEFAULT_REALM"

	//EndpointTemplateForRegionWithDot Environment Variable
	EndpointTemplateForRegionWithDot = "https://{endpoint_service_name}.{region}"

	// Region Metadata
	regionIdentifierPropertyName     = "regionIdentifier"     // e.g. "ap-sydney-1"
	realmKeyPropertyName             = "realmKey"             // e.g. "oc1"
	realmDomainComponentPropertyName = "realmDomainComponent" // e.g. "oraclecloud.com"
	regionKeyPropertyName            = "regionKey"            // e.g. "SYD"

	// OciRealmSpecificServiceEndpointTemplateEnabledEnvVar is the environment variable name to enable the realm specific service endpoint template.
	OciRealmSpecificServiceEndpointTemplateEnabledEnvVar = "OCI_REALM_SPECIFIC_SERVICE_ENDPOINT_TEMPLATE_ENABLED"
)

// External region metadata info flag, used to control adding these metadata region info only once.
var readCfgFile, readEnvVar, visitIMDS bool = true, true, false

// getRegionInfoFromInstanceMetadataService gets the region information
var getRegionInfoFromInstanceMetadataService = getRegionInfoFromInstanceMetadataServiceProd

// OciRealmSpecificServiceEndpointTemplateEnabled is the flag to enable the realm specific service endpoint template. This one has higher priority than the environment variable.
var OciRealmSpecificServiceEndpointTemplateEnabled *bool = nil

// reNonWord precomiles the regex once at the package scope
var reNonWord = regexp.MustCompile(`[^\w]`)

// OciSdkEnabledServicesMap is a list of services that are enabled, default is an empty list which means all services are enabled
var OciSdkEnabledServicesMap map[string]bool

// OciSdkEnabledServicesOnce is a sync.Once variable to ensure the OciSdkEnabledServicesMap is initialized only once
var OciSdkEnabledServicesOnce sync.Once

// OciSdkEnabledServicesMu is a mutex to protect access to the OciSdkEnabledServicesMap
var OciSdkEnabledServicesMu sync.RWMutex

// OciDeveloperToolConfigurationFilePathEnvVar is the environment variable name for the OCI Developer Tool Config File Path
const OciDeveloperToolConfigurationFilePathEnvVar = "OCI_DEVELOPER_TOOL_CONFIGURATION_FILE_PATH"

// OciAllowOnlyDeveloperToolConfigurationRegionsEnvVar is the environment variable name for the OCI Allow only Dev Tool Config Regions
const OciAllowOnlyDeveloperToolConfigurationRegionsEnvVar = "OCI_ALLOW_ONLY_DEVELOPER_TOOL_CONFIGURATION_REGIONS"

// defaultRealmForUnknownDeveloperToolConfigurationRegion is the default realm for unknown Developer Tool Configuration Regions
const defaultRealmForUnknownDeveloperToolConfigurationRegion = "oraclecloud.com"

// OciDeveloperToolConfigurationProvider is the provider name for the OCI Developer Tool Configuration file
var OciDeveloperToolConfigurationProvider string

// ociAllowOnlyDeveloperToolConfigurationRegions is the flag to enable the OCI Allow Only Developer Tool Configuration Regions. This one has lower priority than the environment variable.
var ociAllowOnlyDeveloperToolConfigurationRegions bool

var ociDeveloperToolConfigurationRegionSchemaList []map[string]string

// Endpoint returns a endpoint for a service
func (region Region) Endpoint(service string) string {
	// Endpoint for dotted region
	if strings.Contains(string(region), ".") {
		return fmt.Sprintf("%s.%s", service, region)
	}
	return fmt.Sprintf("%s.%s.%s", service, region, region.SecondLevelDomain())
}

// EndpointForTemplate returns a endpoint for a service based on template, only unknown region name can fall back to "oc1", but not short code region name.
func (region Region) EndpointForTemplate(service string, serviceEndpointTemplate string) string {
	if strings.Contains(string(region), ".") {
		endpoint, error := region.EndpointForTemplateDottedRegion(service, serviceEndpointTemplate, "")
		if error != nil {
			Debugf("%v", error)

			return ""
		}
		return endpoint
	}

	if serviceEndpointTemplate == "" {
		return region.Endpoint(service)
	}

	// replace service prefix
	endpoint := strings.Replace(serviceEndpointTemplate, "{serviceEndpointPrefix}", service, 1)

	// replace region
	endpoint = strings.Replace(endpoint, "{region}", string(region), 1)

	// replace second level domain
	endpoint = strings.Replace(endpoint, "{secondLevelDomain}", region.SecondLevelDomain(), 1)

	return endpoint
}

// EndpointForTemplateDottedRegion returns a endpoint for a service based on the service name and EndpointTemplateForRegionWithDot template. If a service name is missing it is obtained from serviceEndpointTemplate and endpoint is constructed usingEndpointTemplateForRegionWithDot template.
func (region Region) EndpointForTemplateDottedRegion(service string, serviceEndpointTemplate string, endpointServiceName string) (string, error) {
	if !strings.Contains(string(region), ".") {
		var endpoint = ""
		if serviceEndpointTemplate != "" {
			endpoint = region.EndpointForTemplate(service, serviceEndpointTemplate)
			return endpoint, nil
		}
		endpoint = region.EndpointForTemplate(service, "")
		return endpoint, nil
	}

	if endpointServiceName != "" {
		endpoint := strings.Replace(EndpointTemplateForRegionWithDot, "{endpoint_service_name}", endpointServiceName, 1)
		endpoint = strings.Replace(endpoint, "{region}", string(region), 1)
		Debugf("Constructing endpoint from service name %s and region %s. Endpoint: %s", endpointServiceName, region, endpoint)
		return endpoint, nil
	}
	if serviceEndpointTemplate != "" {
		var endpoint = ""
		res := strings.Split(serviceEndpointTemplate, "//")
		if len(res) > 1 {
			res = strings.Split(res[1], ".")
			if len(res) > 1 {
				endpoint = strings.Replace(EndpointTemplateForRegionWithDot, "{endpoint_service_name}", res[0], 1)
				endpoint = strings.Replace(endpoint, "{region}", string(region), 1)
				Debugf("Constructing endpoint from service endpoint template %s and region %s. Endpoint: %s", serviceEndpointTemplate, region, endpoint)
			} else {
				return endpoint, fmt.Errorf("Endpoint service name not present in endpoint template")
			}
		} else {
			return endpoint, fmt.Errorf("invalid serviceEndpointTemplates. ServiceEndpointTemplate should start with https://")
		}
		return endpoint, nil
	}
	return "", fmt.Errorf("EndpointForTemplateDottedRegion function requires endpointServiceName or serviceEndpointTemplate, no endpointServiceName or serviceEndpointTemplate provided")
}

func (region Region) SecondLevelDomain() string {
	if realmID, ok := regionRealm[region]; ok {
		if secondLevelDomain, ok := realm[realmID]; ok {
			return secondLevelDomain
		}
	}
	if value, ok := os.LookupEnv(defaultRealmEnvVarName); ok {
		return value
	}
	Debugf("cannot find realm for region : %s, return default realm value.", region)
	if _, ok := realm["oc1"]; !ok {
		return defaultRealmForUnknownDeveloperToolConfigurationRegion
	}
	return realm["oc1"]
}

// RealmID is used for getting realmID from region, if no region found, directly throw error
func (region Region) RealmID() (string, error) {
	if realmID, ok := regionRealm[region]; ok {
		return realmID, nil
	}

	return "", fmt.Errorf("cannot find realm for region : %s", region)
}

// StringToRegion convert a string to Region type
func StringToRegion(stringRegion string) (r Region) {
	regionStr := strings.ToLower(stringRegion)
	// check for PLC related regions
	if checkAllowOnlyDeveloperToolConfigurationRegions() && (checkDeveloperToolConfigurationFile() || len(ociDeveloperToolConfigurationRegionSchemaList) != 0) {
		Debugf("Developer Tool config detected and OCI_ALLOW_ONLY_DEVELOPER_TOOL_CONFIGURATION_REGIONS is set to True, SDK will only use regions defined for Developer Tool Configuration Regions")
		setRegionMetadataFromDeveloperToolConfigurationFile(&stringRegion)
		if len(ociDeveloperToolConfigurationRegionSchemaList) != 0 {
			resetRegionInfo()
			bulkAddRegionSchema(ociDeveloperToolConfigurationRegionSchemaList)
		}
		r = Region(stringRegion)
		if _, ok := regionRealm[r]; !ok {
			Logf("You're using the %s Developer Tool configuration file, the region you're targeting is not declared in this config file. Please check if this is the correct region you're targeting or contact the %s cloud provider for help. If you want to target both OCI regions and %s regions, please set the OCI_ALLOW_ONLY_DEVELOPER_TOOL_CONFIGURATION_REGIONS env var to False.", OciDeveloperToolConfigurationProvider, OciDeveloperToolConfigurationProvider, regionStr)
		}
		return r
	}

	// check if short region name provided
	if region, ok := shortNameRegion[regionStr]; ok {
		r = region
		return
	}
	// check if normal region name provided
	potentialRegion := Region(regionStr)
	if _, ok := regionRealm[potentialRegion]; ok {
		r = potentialRegion
		return
	}

	Debugf("region named: %s, is not recognized from hard-coded region list, will check Region metadata info", stringRegion)
	r = checkAndAddRegionMetadata(stringRegion)

	return
}

// canStringBeRegion test if the string can be a region, if it can, returns the string as is, otherwise it
// returns an error
var blankRegex = regexp.MustCompile(`\s`)

func canStringBeRegion(stringRegion string) (region string, err error) {
	if blankRegex.MatchString(stringRegion) || stringRegion == "" {
		return "", fmt.Errorf("region can not be empty or have spaces")
	}
	return stringRegion, nil
}

// check region info from original map
func checkAndAddRegionMetadata(region string) Region {
	switch {
	case setRegionMetadataFromCfgFile(&region):
	case setRegionMetadataFromEnvVar(&region):
	case setRegionFromInstanceMetadataService(&region):
	default:
		//err := fmt.Errorf("failed to get region metadata information.")
		return Region(region)
	}
	return Region(region)
}

// EnableInstanceMetadataServiceLookup provides the interface to lookup IMDS region info
func EnableInstanceMetadataServiceLookup() {
	Debugf("Set visitIMDS 'true' to enable IMDS Lookup.")
	visitIMDS = true
}

// setRegionMetadataFromEnvVar checks if region metadata env variable is provided, once it's there, parse and added it
// to region map, and it can make sure the env var can only be visited once.
// Once successfully find the expected region(region name or short code), return true, region name will be stored in
// the input pointer.
func setRegionMetadataFromEnvVar(region *string) bool {
	if !readEnvVar {
		Debugf("metadata region env variable had already been checked, no need to check again.")
		return false //no need to check it again.
	}
	// Mark readEnvVar Flag as false since it has already been visited.
	readEnvVar = false
	// check from env variable
	if jsonStr, existed := os.LookupEnv(regionMetadataEnvVarName); existed {
		Debugf("Raw content of region metadata env var:", jsonStr)
		var regionSchema map[string]string
		if err := json.Unmarshal([]byte(jsonStr), &regionSchema); err != nil {
			Debugf("Can't unmarshal env var, the error info is", err)
			return false
		}
		// check if the specified region is in the env var.
		if checkSchemaItems(regionSchema) {
			// set mapping table
			addRegionSchema(regionSchema)
			if regionSchema[regionKeyPropertyName] == *region ||
				regionSchema[regionIdentifierPropertyName] == *region {
				*region = regionSchema[regionIdentifierPropertyName]
				return true
			}
		}
		return false
	}
	Debugf("The Region Metadata Schema wasn't set in env variable - OCI_REGION_METADATA.")
	return false
}

func setRegionMetadataFromCfgFile(region *string) bool {
	if setRegionMetadataFromDeveloperToolConfigurationFile(region) {
		return true
	}
	if setRegionMetadataFromRegionCfgFile(region) {
		return true
	}
	return false
}

// setRegionMetadataFromCfgFile checks if region metadata config file is provided, once it's there, parse and add all
// the valid regions to region map, the configuration file can only be visited once.
// Once successfully find the expected region(region name or short code), return true, region name will be stored in
// the input pointer.
func setRegionMetadataFromRegionCfgFile(region *string) bool {
	if !readCfgFile {
		Debugf("metadata region config file had already been checked, no need to check again.")
		return false //no need to check it again.
	}
	// Mark readCfgFile Flag as false since it has already been visited.
	readCfgFile = false
	homeFolder := getHomeFolder()
	configFile := filepath.Join(homeFolder, regionMetadataCfgDirName, regionMetadataCfgFileName)
	if jsonArr, ok := readAndParseConfigFile(&configFile); ok {
		added := false
		for _, jsonItem := range jsonArr {
			if checkSchemaItems(jsonItem) {
				addRegionSchema(jsonItem)
				if jsonItem[regionKeyPropertyName] == *region ||
					jsonItem[regionIdentifierPropertyName] == *region {
					*region = jsonItem[regionIdentifierPropertyName]
					added = true
				}
			}
		}
		return added
	}
	return false
}

// setRegionMetadataFromDeveloperToolConfigurationFile checks if Developer Tool config file is provided, once it's there, parse and add all
// The default location of the Developer Tool config file is ~/.oci/developer-tool-configuration.json. It will also check the environment variable
// the valid regions to region map, the configuration file can only be visited once.
// Once successfully find the expected region(region name or short code), return true, region name will be stored in
// the input pointer.
func setRegionMetadataFromDeveloperToolConfigurationFile(region *string) bool {
	if jsonArr, ok := readAndParseDeveloperToolConfigurationFile(); ok {
		added := false
		if jsonArr["regions"] == nil {
			return false
		}
		var regionJSON []map[string]string
		originalJSONContent, err := json.Marshal(jsonArr["regions"])
		if err != nil {
			return false
		}
		err = json.Unmarshal(originalJSONContent, &regionJSON)
		if err != nil {
			return false
		}

		if IsEnvVarTrue(OciAllowOnlyDeveloperToolConfigurationRegionsEnvVar) {
			resetRegionInfo()
		}
		for _, jsonItem := range regionJSON {
			if checkSchemaItems(jsonItem) {
				addRegionSchema(jsonItem)
				if jsonItem[regionKeyPropertyName] == *region ||
					jsonItem[regionIdentifierPropertyName] == *region {
					*region = jsonItem[regionIdentifierPropertyName]
					added = true
				}
			}
		}
		return added
	}
	return false
}

func readAndParseConfigFile(configFileName *string) (fileContent []map[string]string, ok bool) {
	if content, err := ioutil.ReadFile(*configFileName); err == nil {
		Debugf("Raw content of region metadata config file content:", string(content[:]))
		if err := json.Unmarshal(content, &fileContent); err != nil {
			Debugf("Can't unmarshal config file, the error info is", err)
			return
		}
		ok = true
		return
	}
	Debugf("No Region Metadata Config File provided.")
	return
}

func readAndParseDeveloperToolConfigurationFile() (fileContent map[string]interface{}, ok bool) {
	homeFolder := getHomeFolder()
	configFileName := filepath.Join(homeFolder, regionMetadataCfgDirName, "developer-tool-configuration.json")
	if path := os.Getenv(OciDeveloperToolConfigurationFilePathEnvVar); path != "" {
		configFileName = path
	}
	if content, err := ioutil.ReadFile(configFileName); err == nil {
		Debugf("Raw content of Developer Tool config file content:", string(content[:]))
		if err := json.Unmarshal(content, &fileContent); err != nil {
			Debugf("Can't unmarshal env var, the error info is", err)
			return
		}
		ok = true
		return
	}
	Debugf("No Developer Tool Config File provided.")
	return
}

func checkDeveloperToolConfigurationFile() bool {
	homeFolder := getHomeFolder()
	configFileName := filepath.Join(homeFolder, regionMetadataCfgDirName, "developer-tool-configuration.json")
	if path := os.Getenv(OciDeveloperToolConfigurationFilePathEnvVar); path != "" {
		configFileName = path
	}
	if _, err := os.Stat(configFileName); err == nil {
		return true
	}
	return false
}

// check map regionRealm's region name, if it's already there, no need to add it.
func addRegionSchema(regionSchema map[string]string) {
	r := Region(strings.ToLower(regionSchema[regionIdentifierPropertyName]))
	if _, ok := regionRealm[r]; !ok {
		// set mapping table
		shortNameRegion[regionSchema[regionKeyPropertyName]] = r
		realm[regionSchema[realmKeyPropertyName]] = regionSchema[realmDomainComponentPropertyName]
		regionRealm[r] = regionSchema[realmKeyPropertyName]
		return
	}
	Debugf("Region {} has already been added, no need to add again.", regionSchema[regionIdentifierPropertyName])
}

// AddRegionSchemaForPlc add region schema to region map
func AddRegionSchemaForPlc(regionSchema map[string]string) {
	ociDeveloperToolConfigurationRegionSchemaList = append(ociDeveloperToolConfigurationRegionSchemaList, regionSchema)
	addRegionSchema(regionSchema)
	// if !IsEnvVarTrue(OciPlcRegionExclusiveEnvVar) {
	// 	addRegionSchema(regionSchema)
	// 	return
	// }
	// Debugf("Plc region coexist is not enabled, remove exisiting OCI region schema and add PLC region schema.")
	// resetRegionInfo()
	// bulkAddRegionSchema(ociPlcRegionSchemaList)
}

func resetRegionInfo() {
	shortNameRegion = make(map[string]Region)
	realm = make(map[string]string)
	regionRealm = make(map[Region]string)
}

func bulkAddRegionSchema(regionSchemaList []map[string]string) {
	for _, regionSchema := range regionSchemaList {
		if checkSchemaItems(regionSchema) {
			addRegionSchema(regionSchema)
		}
	}
}

// check region schema content if all the required contents are provided
func checkSchemaItems(regionSchema map[string]string) bool {
	if checkSchemaItem(regionSchema, regionIdentifierPropertyName) &&
		checkSchemaItem(regionSchema, realmKeyPropertyName) &&
		checkSchemaItem(regionSchema, realmDomainComponentPropertyName) &&
		checkSchemaItem(regionSchema, regionKeyPropertyName) {
		return true
	}
	return false
}

// check region schema item is valid, if so, convert it to lower case.
func checkSchemaItem(regionSchema map[string]string, key string) bool {
	if val, ok := regionSchema[key]; ok {
		if val != "" {
			regionSchema[key] = strings.ToLower(val)
			return true
		}
		Debugf("Region metadata schema {} is provided,but content is empty.", key)
		return false
	}
	Debugf("Region metadata schema {} is not provided, please update the content", key)
	return false
}

// setRegionFromInstanceMetadataService checks if region metadata can be provided from InstanceMetadataService.
// Once successfully find the expected region(region name or short code), return true, region name will be stored in
// the input pointer.
// setRegionFromInstanceMetadataService will only be checked on the instance, by default it will not be enabled unless
// user explicitly enable it.
func setRegionFromInstanceMetadataService(region *string) bool {
	// example of content:
	// {
	// 	"realmKey" : "oc1",
	// 	"realmDomainComponent" : "oraclecloud.com",
	// 	"regionKey" : "YUL",
	// 	"regionIdentifier" : "ca-montreal-1"
	// }
	// Mark visitIMDS Flag as false since it has already been visited.
	if !visitIMDS {
		Debugf("check from IMDS is disabled or IMDS had already been successfully visited, no need to check again.")
		return false
	}
	content, err := getRegionInfoFromInstanceMetadataService()
	if err != nil {
		Debugf("Failed to get instance metadata. Error: %v", err)
		return false
	}

	// Mark visitIMDS Flag as false since we have already successfully get the region info from IMDS.
	visitIMDS = false

	var regionInfo map[string]string
	err = json.Unmarshal(content, &regionInfo)
	if err != nil {
		Debugf("Failed to unmarshal the response content: %v \nError: %v", string(content), err)
		return false
	}

	if checkSchemaItems(regionInfo) {
		addRegionSchema(regionInfo)
		if regionInfo[regionKeyPropertyName] == *region ||
			regionInfo[regionIdentifierPropertyName] == *region {
			*region = regionInfo[regionIdentifierPropertyName]
		}
	} else {
		Debugf("Region information is not valid.")
		return false
	}

	return true
}

// getRegionInfoFromInstanceMetadataServiceProd calls instance metadata service and get the region information
func getRegionInfoFromInstanceMetadataServiceProd() ([]byte, error) {
	request, _ := http.NewRequest(http.MethodGet, instanceMetadataRegionInfoURLV2, nil)
	request.Header.Add("Authorization", "Bearer Oracle")

	client := &http.Client{
		Timeout: time.Second * 10,
	}
	resp, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("failed to call instance metadata service. Error: %v", err)
	}

	statusCode := resp.StatusCode

	defer resp.Body.Close()

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to get region information from response body. Error: %v", err)
	}

	if statusCode != http.StatusOK {
		err = fmt.Errorf("HTTP Get failed: URL: %s, Status: %s, Message: %s",
			instanceMetadataRegionInfoURLV2, resp.Status, string(content))
		return nil, err
	}

	return content, nil
}

// TemplateParamForPerRealmEndpoint is a template parameter for per-realm endpoint.
type TemplateParamForPerRealmEndpoint struct {
	Template    string
	EndsWithDot bool
}

// SetMissingTemplateParams function will parse the {} template in client host and replace with empty string.
func SetMissingTemplateParams(client *BaseClient) {
	templateRegex := regexp.MustCompile(`{.*?}`)
	templates := templateRegex.FindAllString(client.Host, -1)
	for _, template := range templates {
		client.Host = strings.Replace(client.Host, template, "", -1)
	}
}

func getOciSdkEnabledServicesMap() map[string]bool {
	var enabledMap = make(map[string]bool)
	if jsonArr, ok := readAndParseDeveloperToolConfigurationFile(); ok {
		if jsonArr["provider"] != nil {
			OciDeveloperToolConfigurationProvider = jsonArr["provider"].(string)
		}
		if jsonArr["allowOnlyDeveloperToolConfigurationRegions"] != nil && jsonArr["allowOnlyDeveloperToolConfigurationRegions"] == false {
			ociAllowOnlyDeveloperToolConfigurationRegions = jsonArr["allowOnlyDeveloperToolConfigurationRegions"].(bool)
		}
		if jsonArr["services"] == nil {
			return enabledMap
		}
		serviesJSON, ok := jsonArr["services"].([]interface{})
		if !ok {
			return enabledMap
		}
		re, _ := regexp.Compile(`[^\w]`)
		for _, jsonItem := range serviesJSON {
			serviceName := strings.ToLower(fmt.Sprint(jsonItem))
			serviceName = re.ReplaceAllString(serviceName, "")
			enabledMap[serviceName] = true
		}
	}
	return enabledMap
}

// AddServiceToEnabledServicesMap adds the service to the enabledServiceMap
// The service name will auto transit to lower case and remove all the non-word characters.
// Concurrency (goroutine-safe). The map is initialized with sync.Once, and writes are protected by a RWMutex.
func AddServiceToEnabledServicesMap(serviceName string) {
	OciSdkEnabledServicesOnce.Do(func() {
		OciSdkEnabledServicesMap = getOciSdkEnabledServicesMap()
		if OciSdkEnabledServicesMap == nil {
			OciSdkEnabledServicesMap = make(map[string]bool)
		}
	})
	serviceName = strings.ToLower(serviceName)
	serviceName = reNonWord.ReplaceAllString(serviceName, "")

	OciSdkEnabledServicesMu.Lock()
	defer OciSdkEnabledServicesMu.Unlock()
	OciSdkEnabledServicesMap[serviceName] = true
}

// CheckForEnabledServices checks if the service is enabled in the enabledServiceMap.
// It will first check if the map is initialized, if not, it will initialize the map.
// If the map is empty, it means all the services are enabled.
// If the map is not empty, it means only the services in the map and value is true are enabled.
// Concurrency (goroutine-safe). Initialization uses sync.Once and reads are protected by a RWMutex.
func CheckForEnabledServices(serviceName string) bool {
	OciSdkEnabledServicesOnce.Do(func() {
		OciSdkEnabledServicesMap = getOciSdkEnabledServicesMap()
		if OciSdkEnabledServicesMap == nil {
			OciSdkEnabledServicesMap = make(map[string]bool)
		}
	})
	serviceName = strings.ToLower(serviceName)
	serviceName = reNonWord.ReplaceAllString(serviceName, "")

	OciSdkEnabledServicesMu.RLock()
	defer OciSdkEnabledServicesMu.RUnlock()

	if len(OciSdkEnabledServicesMap) == 0 {
		return true
	}
	allowed, ok := OciSdkEnabledServicesMap[serviceName]
	if !ok {
		return false
	}
	return allowed
}

// CheckAllowOnlyDeveloperToolConfigurationRegions checks if only developer tool configuration regions are allowed
// This function will first check if the OCI_ALLOW_ONLY_DEVELOPER_TOOL_CONFIGURATION_REGIONS environment variable is set.
// If it is set, it will return the value.
// If it is not set, it will return the value from the ociAllowOnlyDeveloperToolConfigurationRegions variable.
func checkAllowOnlyDeveloperToolConfigurationRegions() bool {
	if val, ok := os.LookupEnv("OCI_ALLOW_ONLY_DEVELOPER_TOOL_CONFIGURATION_REGIONS"); ok {
		return val == "true"
	}
	return ociAllowOnlyDeveloperToolConfigurationRegions
}
