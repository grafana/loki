package core

// (C) Copyright IBM Corp. 2019, 2022.
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

const (
	// Supported authentication types.
	AUTHTYPE_BASIC        = "basic"
	AUTHTYPE_BEARER_TOKEN = "bearerToken"
	AUTHTYPE_NOAUTH       = "noAuth"
	AUTHTYPE_IAM          = "iam"
	AUTHTYPE_CP4D         = "cp4d"
	AUTHTYPE_CONTAINER    = "container"
	AUTHTYPE_VPC          = "vpc"

	// Names of properties that can be defined as part of an external configuration (credential file, env vars, etc.).
	// Example:  export MYSERVICE_URL=https://myurl

	// Service client properties.
	PROPNAME_SVC_URL            = "URL"
	PROPNAME_SVC_DISABLE_SSL    = "DISABLE_SSL"
	PROPNAME_SVC_ENABLE_GZIP    = "ENABLE_GZIP"
	PROPNAME_SVC_ENABLE_RETRIES = "ENABLE_RETRIES"
	PROPNAME_SVC_MAX_RETRIES    = "MAX_RETRIES"
	PROPNAME_SVC_RETRY_INTERVAL = "RETRY_INTERVAL"

	// Authenticator properties.
	PROPNAME_AUTH_TYPE        = "AUTH_TYPE"
	PROPNAME_USERNAME         = "USERNAME"
	PROPNAME_PASSWORD         = "PASSWORD"
	PROPNAME_BEARER_TOKEN     = "BEARER_TOKEN"
	PROPNAME_AUTH_URL         = "AUTH_URL"
	PROPNAME_AUTH_DISABLE_SSL = "AUTH_DISABLE_SSL"
	PROPNAME_APIKEY           = "APIKEY"
	PROPNAME_REFRESH_TOKEN    = "REFRESH_TOKEN" // #nosec G101
	PROPNAME_CLIENT_ID        = "CLIENT_ID"
	PROPNAME_CLIENT_SECRET    = "CLIENT_SECRET"
	PROPNAME_SCOPE            = "SCOPE"
	PROPNAME_CRTOKEN_FILENAME = "CR_TOKEN_FILENAME" // #nosec G101
	PROPNAME_IAM_PROFILE_CRN  = "IAM_PROFILE_CRN"
	PROPNAME_IAM_PROFILE_NAME = "IAM_PROFILE_NAME"
	PROPNAME_IAM_PROFILE_ID   = "IAM_PROFILE_ID"

	// SSL error
	SSL_CERTIFICATION_ERROR = "x509: certificate"

	// Common error messages.
	ERRORMSG_PROP_MISSING            = "The %s property is required but was not specified."
	ERRORMSG_PROP_INVALID            = "The %s property is invalid. Please remove any surrounding {, }, or \" characters."
	ERRORMSG_EXCLUSIVE_PROPS_ERROR   = "Exactly one of %s or %s must be specified."
	ERRORMSG_ATLEAST_ONE_PROP_ERROR  = "At least one of %s or %s must be specified."
	ERRORMSG_ATMOST_ONE_PROP_ERROR   = "At most one of %s or %s may be specified."
	ERRORMSG_NO_AUTHENTICATOR        = "Authentication information was not properly configured."
	ERRORMSG_AUTHTYPE_UNKNOWN        = "Unrecognized authentication type: %s"
	ERRORMSG_PROPS_MAP_NIL           = "The 'properties' map cannot be nil."
	ERRORMSG_SSL_VERIFICATION_FAILED = "The connection failed because the SSL certificate is not valid. To use a " +
		"self-signed certificate, disable verification of the server's SSL certificate " +
		"by invoking the DisableSSLVerification() function on your service instance " +
		"and/or use the DisableSSLVerification option of the authenticator."
	ERRORMSG_AUTHENTICATE_ERROR      = "An error occurred while performing the 'authenticate' step: %s"
	ERRORMSG_READ_RESPONSE_BODY      = "An error occurred while reading the response body: %s"
	ERRORMSG_UNEXPECTED_RESPONSE     = "The response contained unexpected content, Content-Type=%s, operation resultType=%s"
	ERRORMSG_UNMARSHAL_RESPONSE_BODY = "An error occurred while unmarshalling the response body: %s"
	ERRORMSG_NIL_SLICE               = "The 'slice' parameter cannot be nil"
	ERRORMSG_PARAM_NOT_SLICE         = "The 'slice' parameter must be a slice"
	ERRORMSG_MARSHAL_SLICE           = "An error occurred while marshalling the slice: %s"
	ERRORMSG_CONVERT_SLICE           = "An error occurred while converting 'slice' to string slice"
	ERRORMSG_UNEXPECTED_STATUS_CODE  = "Unexpected HTTP status code %d (%s)"
	ERRORMSG_UNMARSHAL_AUTH_RESPONSE = "error unmarshalling authentication response: %s"
	ERRORMSG_UNABLE_RETRIEVE_CRTOKEN = "unable to retrieve compute resource token value: %s"          // #nosec G101
	ERRORMSG_IAM_GETTOKEN_ERROR      = "IAM 'get token' error, status code %d received from '%s': %s" // #nosec G101
	ERRORMSG_UNABLE_RETRIEVE_IITOKEN = "unable to retrieve instance identity token value: %s"         // #nosec G101
	ERRORMSG_VPCMDS_OPERATION_ERROR  = "VPC metadata service error, status code %d received from '%s': %s"
)
