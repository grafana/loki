package ibmiam

// IBM COS SDK Code -- START
import (
	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/request"
)

// SignRequestHandler is a named request handler the SDK will use to sign
// service client request with using the IBM IAM signature.
var SignRequestHandler = request.NamedHandler{
	Name: signRequestHandlerLog, Fn: Sign,
}

var (
	// Errors for Sign Request Handler
	errTokenTypeNotSet         = awserr.New(signRequestHandlerLog, "Token Type Not Set", nil)
	errAccessTokenNotSet       = awserr.New(signRequestHandlerLog, "Access Token Not Set", nil)
	errServiceInstanceIDNotSet = awserr.New(signRequestHandlerLog, "Service Instance Id Not Set", nil)
)

// Sign signs IBM IAM requests with the token type, access token and service
// instance id request is made to, and time the request is signed at.
//
// Returns a list of HTTP headers that were included in the signature or an
// error if signing the request failed. Generally for signed requests this value
// is not needed as the full request context will be captured by the http.Request
// value. It is included for reference though.
func Sign(req *request.Request) {

	// Sets the logger for the Request to be signed
	logger := req.Config.Logger
	if !req.Config.LogLevel.Matches(aws.LogDebug) {
		logger = nil
	}

	// Obtains the IBM IAM Credentials Object
	// The objects includes:
	//		IBM IAM Token
	//		IBM IAM Service Instance ID
	value, err := req.Config.Credentials.Get()
	if err != nil {
		if logger != nil {
			logger.Log(debugLog, signRequestHandlerLog, "CREDENTIAL GET ERROR", err)
		}
		req.Error = err
		req.SignedHeaderVals = nil
		return
	}

	// Check the type of the Token
	// If does not exist, return with an error in the request
	if value.TokenType == "" {
		err = errTokenTypeNotSet
		if logger != nil {
			logger.Log(debugLog, err)
		}
		req.Error = err
		req.SignedHeaderVals = nil
		return
	}

	// Checks the Access Token
	// If does not exist, return with an error in the request
	if value.AccessToken == "" {
		err = errAccessTokenNotSet
		if logger != nil {
			logger.Log(debugLog, err)
		}
		req.Error = err
		req.SignedHeaderVals = nil
		return
	}

	// Get the Service Instance ID from the IBM IAM Credentials object
	serviceInstanceID := req.HTTPRequest.Header.Get("ibm-service-instance-id")
	if serviceInstanceID == "" && value.ServiceInstanceID != "" {
		// Log the Service Instance ID
		if logger != nil {
			logger.Log(debugLog, "Setting the 'ibm-service-instance-id' from the Credentials")

		}
		req.HTTPRequest.Header.Set("ibm-service-instance-id", value.ServiceInstanceID)
	}

	// Use the IBM IAM Token Bearer as the Authorization Header
	authString := value.TokenType + " " + value.AccessToken
	req.HTTPRequest.Header.Set("Authorization", authString)
	if logger != nil {
		logger.Log(debugLog, signRequestHandlerLog, "Set Header Authorization", authString)
	}
}

// IBM COS SDK Code -- END
