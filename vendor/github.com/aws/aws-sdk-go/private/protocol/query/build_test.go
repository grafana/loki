package query_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/client/metadata"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/aws/aws-sdk-go/awstesting"
	"github.com/aws/aws-sdk-go/awstesting/unit"
	"github.com/aws/aws-sdk-go/private/protocol"
	"github.com/aws/aws-sdk-go/private/protocol/query"
	"github.com/aws/aws-sdk-go/private/protocol/xml/xmlutil"
	"github.com/aws/aws-sdk-go/private/util"
)

var _ bytes.Buffer // always import bytes
var _ http.Request
var _ json.Marshaler
var _ time.Time
var _ xmlutil.XMLNode
var _ xml.Attr
var _ = ioutil.Discard
var _ = util.Trim("")
var _ = url.Values{}
var _ = io.EOF
var _ = aws.String
var _ = fmt.Println
var _ = reflect.Value{}

func init() {
	protocol.RandReader = &awstesting.ZeroReader{}
}

// InputService1ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService1ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService1ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService1ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService1ProtocolTest client from just a session.
//     svc := inputservice1protocoltest.New(mySession)
//
//     // Create a InputService1ProtocolTest client with additional configuration
//     svc := inputservice1protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService1ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService1ProtocolTest {
	c := p.ClientConfig("inputservice1protocoltest", cfgs...)
	return newInputService1ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService1ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService1ProtocolTest {
	svc := &InputService1ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService1ProtocolTest",
				ServiceID:     "InputService1ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService1ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService1ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService1TestCaseOperation1 = "OperationName"

// InputService1TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService1TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService1TestCaseOperation1 for more information on using the InputService1TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService1TestCaseOperation1Request method.
//    req, resp := client.InputService1TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService1ProtocolTest) InputService1TestCaseOperation1Request(input *InputService1TestShapeInputService1TestCaseOperation1Input) (req *request.Request, output *InputService1TestShapeInputService1TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService1TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService1TestShapeInputService1TestCaseOperation1Input{}
	}

	output = &InputService1TestShapeInputService1TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService1TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService1TestCaseOperation1 for usage and error information.
func (c *InputService1ProtocolTest) InputService1TestCaseOperation1(input *InputService1TestShapeInputService1TestCaseOperation1Input) (*InputService1TestShapeInputService1TestCaseOperation1Output, error) {
	req, out := c.InputService1TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService1TestCaseOperation1WithContext is the same as InputService1TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService1TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService1ProtocolTest) InputService1TestCaseOperation1WithContext(ctx aws.Context, input *InputService1TestShapeInputService1TestCaseOperation1Input, opts ...request.Option) (*InputService1TestShapeInputService1TestCaseOperation1Output, error) {
	req, out := c.InputService1TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService1TestCaseOperation2 = "OperationName"

// InputService1TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService1TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService1TestCaseOperation2 for more information on using the InputService1TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService1TestCaseOperation2Request method.
//    req, resp := client.InputService1TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService1ProtocolTest) InputService1TestCaseOperation2Request(input *InputService1TestShapeInputService1TestCaseOperation2Input) (req *request.Request, output *InputService1TestShapeInputService1TestCaseOperation2Output) {
	op := &request.Operation{
		Name:     opInputService1TestCaseOperation2,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService1TestShapeInputService1TestCaseOperation2Input{}
	}

	output = &InputService1TestShapeInputService1TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService1TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService1TestCaseOperation2 for usage and error information.
func (c *InputService1ProtocolTest) InputService1TestCaseOperation2(input *InputService1TestShapeInputService1TestCaseOperation2Input) (*InputService1TestShapeInputService1TestCaseOperation2Output, error) {
	req, out := c.InputService1TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService1TestCaseOperation2WithContext is the same as InputService1TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService1TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService1ProtocolTest) InputService1TestCaseOperation2WithContext(ctx aws.Context, input *InputService1TestShapeInputService1TestCaseOperation2Input, opts ...request.Option) (*InputService1TestShapeInputService1TestCaseOperation2Output, error) {
	req, out := c.InputService1TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService1TestCaseOperation3 = "OperationName"

// InputService1TestCaseOperation3Request generates a "aws/request.Request" representing the
// client's request for the InputService1TestCaseOperation3 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService1TestCaseOperation3 for more information on using the InputService1TestCaseOperation3
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService1TestCaseOperation3Request method.
//    req, resp := client.InputService1TestCaseOperation3Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService1ProtocolTest) InputService1TestCaseOperation3Request(input *InputService1TestShapeInputService1TestCaseOperation3Input) (req *request.Request, output *InputService1TestShapeInputService1TestCaseOperation3Output) {
	op := &request.Operation{
		Name:     opInputService1TestCaseOperation3,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService1TestShapeInputService1TestCaseOperation3Input{}
	}

	output = &InputService1TestShapeInputService1TestCaseOperation3Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService1TestCaseOperation3 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService1TestCaseOperation3 for usage and error information.
func (c *InputService1ProtocolTest) InputService1TestCaseOperation3(input *InputService1TestShapeInputService1TestCaseOperation3Input) (*InputService1TestShapeInputService1TestCaseOperation3Output, error) {
	req, out := c.InputService1TestCaseOperation3Request(input)
	return out, req.Send()
}

// InputService1TestCaseOperation3WithContext is the same as InputService1TestCaseOperation3 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService1TestCaseOperation3 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService1ProtocolTest) InputService1TestCaseOperation3WithContext(ctx aws.Context, input *InputService1TestShapeInputService1TestCaseOperation3Input, opts ...request.Option) (*InputService1TestShapeInputService1TestCaseOperation3Output, error) {
	req, out := c.InputService1TestCaseOperation3Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService1TestShapeInputService1TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	Bar *string `type:"string"`

	Baz *bool `type:"boolean"`

	Foo *string `type:"string"`
}

// SetBar sets the Bar field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation1Input) SetBar(v string) *InputService1TestShapeInputService1TestCaseOperation1Input {
	s.Bar = &v
	return s
}

// SetBaz sets the Baz field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation1Input) SetBaz(v bool) *InputService1TestShapeInputService1TestCaseOperation1Input {
	s.Baz = &v
	return s
}

// SetFoo sets the Foo field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation1Input) SetFoo(v string) *InputService1TestShapeInputService1TestCaseOperation1Input {
	s.Foo = &v
	return s
}

type InputService1TestShapeInputService1TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService1TestShapeInputService1TestCaseOperation2Input struct {
	_ struct{} `type:"structure"`

	Bar *string `type:"string"`

	Baz *bool `type:"boolean"`

	Foo *string `type:"string"`
}

// SetBar sets the Bar field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation2Input) SetBar(v string) *InputService1TestShapeInputService1TestCaseOperation2Input {
	s.Bar = &v
	return s
}

// SetBaz sets the Baz field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation2Input) SetBaz(v bool) *InputService1TestShapeInputService1TestCaseOperation2Input {
	s.Baz = &v
	return s
}

// SetFoo sets the Foo field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation2Input) SetFoo(v string) *InputService1TestShapeInputService1TestCaseOperation2Input {
	s.Foo = &v
	return s
}

type InputService1TestShapeInputService1TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

type InputService1TestShapeInputService1TestCaseOperation3Input struct {
	_ struct{} `type:"structure"`

	Bar *string `type:"string"`

	Baz *bool `type:"boolean"`

	Foo *string `type:"string"`
}

// SetBar sets the Bar field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation3Input) SetBar(v string) *InputService1TestShapeInputService1TestCaseOperation3Input {
	s.Bar = &v
	return s
}

// SetBaz sets the Baz field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation3Input) SetBaz(v bool) *InputService1TestShapeInputService1TestCaseOperation3Input {
	s.Baz = &v
	return s
}

// SetFoo sets the Foo field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation3Input) SetFoo(v string) *InputService1TestShapeInputService1TestCaseOperation3Input {
	s.Foo = &v
	return s
}

type InputService1TestShapeInputService1TestCaseOperation3Output struct {
	_ struct{} `type:"structure"`
}

// InputService2ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService2ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService2ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService2ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService2ProtocolTest client from just a session.
//     svc := inputservice2protocoltest.New(mySession)
//
//     // Create a InputService2ProtocolTest client with additional configuration
//     svc := inputservice2protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService2ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService2ProtocolTest {
	c := p.ClientConfig("inputservice2protocoltest", cfgs...)
	return newInputService2ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService2ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService2ProtocolTest {
	svc := &InputService2ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService2ProtocolTest",
				ServiceID:     "InputService2ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService2ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService2ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService2TestCaseOperation1 = "OperationName"

// InputService2TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService2TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService2TestCaseOperation1 for more information on using the InputService2TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService2TestCaseOperation1Request method.
//    req, resp := client.InputService2TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService2ProtocolTest) InputService2TestCaseOperation1Request(input *InputService2TestShapeInputService2TestCaseOperation1Input) (req *request.Request, output *InputService2TestShapeInputService2TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService2TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService2TestShapeInputService2TestCaseOperation1Input{}
	}

	output = &InputService2TestShapeInputService2TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService2TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService2TestCaseOperation1 for usage and error information.
func (c *InputService2ProtocolTest) InputService2TestCaseOperation1(input *InputService2TestShapeInputService2TestCaseOperation1Input) (*InputService2TestShapeInputService2TestCaseOperation1Output, error) {
	req, out := c.InputService2TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService2TestCaseOperation1WithContext is the same as InputService2TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService2TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService2ProtocolTest) InputService2TestCaseOperation1WithContext(ctx aws.Context, input *InputService2TestShapeInputService2TestCaseOperation1Input, opts ...request.Option) (*InputService2TestShapeInputService2TestCaseOperation1Output, error) {
	req, out := c.InputService2TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService2TestShapeInputService2TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	StructArg *InputService2TestShapeStructType `type:"structure"`
}

// SetStructArg sets the StructArg field's value.
func (s *InputService2TestShapeInputService2TestCaseOperation1Input) SetStructArg(v *InputService2TestShapeStructType) *InputService2TestShapeInputService2TestCaseOperation1Input {
	s.StructArg = v
	return s
}

type InputService2TestShapeInputService2TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService2TestShapeStructType struct {
	_ struct{} `type:"structure"`

	ScalarArg *string `type:"string"`
}

// SetScalarArg sets the ScalarArg field's value.
func (s *InputService2TestShapeStructType) SetScalarArg(v string) *InputService2TestShapeStructType {
	s.ScalarArg = &v
	return s
}

// InputService3ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService3ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService3ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService3ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService3ProtocolTest client from just a session.
//     svc := inputservice3protocoltest.New(mySession)
//
//     // Create a InputService3ProtocolTest client with additional configuration
//     svc := inputservice3protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService3ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService3ProtocolTest {
	c := p.ClientConfig("inputservice3protocoltest", cfgs...)
	return newInputService3ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService3ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService3ProtocolTest {
	svc := &InputService3ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService3ProtocolTest",
				ServiceID:     "InputService3ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService3ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService3ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService3TestCaseOperation1 = "OperationName"

// InputService3TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService3TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService3TestCaseOperation1 for more information on using the InputService3TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService3TestCaseOperation1Request method.
//    req, resp := client.InputService3TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService3ProtocolTest) InputService3TestCaseOperation1Request(input *InputService3TestShapeInputService3TestCaseOperation1Input) (req *request.Request, output *InputService3TestShapeInputService3TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService3TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService3TestShapeInputService3TestCaseOperation1Input{}
	}

	output = &InputService3TestShapeInputService3TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService3TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService3TestCaseOperation1 for usage and error information.
func (c *InputService3ProtocolTest) InputService3TestCaseOperation1(input *InputService3TestShapeInputService3TestCaseOperation1Input) (*InputService3TestShapeInputService3TestCaseOperation1Output, error) {
	req, out := c.InputService3TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService3TestCaseOperation1WithContext is the same as InputService3TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService3TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService3ProtocolTest) InputService3TestCaseOperation1WithContext(ctx aws.Context, input *InputService3TestShapeInputService3TestCaseOperation1Input, opts ...request.Option) (*InputService3TestShapeInputService3TestCaseOperation1Output, error) {
	req, out := c.InputService3TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService3TestCaseOperation2 = "OperationName"

// InputService3TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService3TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService3TestCaseOperation2 for more information on using the InputService3TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService3TestCaseOperation2Request method.
//    req, resp := client.InputService3TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService3ProtocolTest) InputService3TestCaseOperation2Request(input *InputService3TestShapeInputService3TestCaseOperation2Input) (req *request.Request, output *InputService3TestShapeInputService3TestCaseOperation2Output) {
	op := &request.Operation{
		Name:     opInputService3TestCaseOperation2,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService3TestShapeInputService3TestCaseOperation2Input{}
	}

	output = &InputService3TestShapeInputService3TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService3TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService3TestCaseOperation2 for usage and error information.
func (c *InputService3ProtocolTest) InputService3TestCaseOperation2(input *InputService3TestShapeInputService3TestCaseOperation2Input) (*InputService3TestShapeInputService3TestCaseOperation2Output, error) {
	req, out := c.InputService3TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService3TestCaseOperation2WithContext is the same as InputService3TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService3TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService3ProtocolTest) InputService3TestCaseOperation2WithContext(ctx aws.Context, input *InputService3TestShapeInputService3TestCaseOperation2Input, opts ...request.Option) (*InputService3TestShapeInputService3TestCaseOperation2Output, error) {
	req, out := c.InputService3TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService3TestShapeInputService3TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	ListArg []*string `type:"list"`
}

// SetListArg sets the ListArg field's value.
func (s *InputService3TestShapeInputService3TestCaseOperation1Input) SetListArg(v []*string) *InputService3TestShapeInputService3TestCaseOperation1Input {
	s.ListArg = v
	return s
}

type InputService3TestShapeInputService3TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService3TestShapeInputService3TestCaseOperation2Input struct {
	_ struct{} `type:"structure"`

	ListArg []*string `type:"list"`
}

// SetListArg sets the ListArg field's value.
func (s *InputService3TestShapeInputService3TestCaseOperation2Input) SetListArg(v []*string) *InputService3TestShapeInputService3TestCaseOperation2Input {
	s.ListArg = v
	return s
}

type InputService3TestShapeInputService3TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

// InputService4ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService4ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService4ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService4ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService4ProtocolTest client from just a session.
//     svc := inputservice4protocoltest.New(mySession)
//
//     // Create a InputService4ProtocolTest client with additional configuration
//     svc := inputservice4protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService4ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService4ProtocolTest {
	c := p.ClientConfig("inputservice4protocoltest", cfgs...)
	return newInputService4ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService4ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService4ProtocolTest {
	svc := &InputService4ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService4ProtocolTest",
				ServiceID:     "InputService4ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService4ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService4ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService4TestCaseOperation1 = "OperationName"

// InputService4TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService4TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService4TestCaseOperation1 for more information on using the InputService4TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService4TestCaseOperation1Request method.
//    req, resp := client.InputService4TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService4ProtocolTest) InputService4TestCaseOperation1Request(input *InputService4TestShapeInputService4TestCaseOperation1Input) (req *request.Request, output *InputService4TestShapeInputService4TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService4TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService4TestShapeInputService4TestCaseOperation1Input{}
	}

	output = &InputService4TestShapeInputService4TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService4TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService4TestCaseOperation1 for usage and error information.
func (c *InputService4ProtocolTest) InputService4TestCaseOperation1(input *InputService4TestShapeInputService4TestCaseOperation1Input) (*InputService4TestShapeInputService4TestCaseOperation1Output, error) {
	req, out := c.InputService4TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService4TestCaseOperation1WithContext is the same as InputService4TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService4TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService4ProtocolTest) InputService4TestCaseOperation1WithContext(ctx aws.Context, input *InputService4TestShapeInputService4TestCaseOperation1Input, opts ...request.Option) (*InputService4TestShapeInputService4TestCaseOperation1Output, error) {
	req, out := c.InputService4TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService4TestCaseOperation2 = "OperationName"

// InputService4TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService4TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService4TestCaseOperation2 for more information on using the InputService4TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService4TestCaseOperation2Request method.
//    req, resp := client.InputService4TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService4ProtocolTest) InputService4TestCaseOperation2Request(input *InputService4TestShapeInputService4TestCaseOperation2Input) (req *request.Request, output *InputService4TestShapeInputService4TestCaseOperation2Output) {
	op := &request.Operation{
		Name:     opInputService4TestCaseOperation2,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService4TestShapeInputService4TestCaseOperation2Input{}
	}

	output = &InputService4TestShapeInputService4TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService4TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService4TestCaseOperation2 for usage and error information.
func (c *InputService4ProtocolTest) InputService4TestCaseOperation2(input *InputService4TestShapeInputService4TestCaseOperation2Input) (*InputService4TestShapeInputService4TestCaseOperation2Output, error) {
	req, out := c.InputService4TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService4TestCaseOperation2WithContext is the same as InputService4TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService4TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService4ProtocolTest) InputService4TestCaseOperation2WithContext(ctx aws.Context, input *InputService4TestShapeInputService4TestCaseOperation2Input, opts ...request.Option) (*InputService4TestShapeInputService4TestCaseOperation2Output, error) {
	req, out := c.InputService4TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService4TestShapeInputService4TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	ListArg []*string `type:"list" flattened:"true"`

	NamedListArg []*string `locationNameList:"Foo" type:"list" flattened:"true"`

	ScalarArg *string `type:"string"`
}

// SetListArg sets the ListArg field's value.
func (s *InputService4TestShapeInputService4TestCaseOperation1Input) SetListArg(v []*string) *InputService4TestShapeInputService4TestCaseOperation1Input {
	s.ListArg = v
	return s
}

// SetNamedListArg sets the NamedListArg field's value.
func (s *InputService4TestShapeInputService4TestCaseOperation1Input) SetNamedListArg(v []*string) *InputService4TestShapeInputService4TestCaseOperation1Input {
	s.NamedListArg = v
	return s
}

// SetScalarArg sets the ScalarArg field's value.
func (s *InputService4TestShapeInputService4TestCaseOperation1Input) SetScalarArg(v string) *InputService4TestShapeInputService4TestCaseOperation1Input {
	s.ScalarArg = &v
	return s
}

type InputService4TestShapeInputService4TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService4TestShapeInputService4TestCaseOperation2Input struct {
	_ struct{} `type:"structure"`

	ListArg []*string `type:"list" flattened:"true"`

	NamedListArg []*string `locationNameList:"Foo" type:"list" flattened:"true"`

	ScalarArg *string `type:"string"`
}

// SetListArg sets the ListArg field's value.
func (s *InputService4TestShapeInputService4TestCaseOperation2Input) SetListArg(v []*string) *InputService4TestShapeInputService4TestCaseOperation2Input {
	s.ListArg = v
	return s
}

// SetNamedListArg sets the NamedListArg field's value.
func (s *InputService4TestShapeInputService4TestCaseOperation2Input) SetNamedListArg(v []*string) *InputService4TestShapeInputService4TestCaseOperation2Input {
	s.NamedListArg = v
	return s
}

// SetScalarArg sets the ScalarArg field's value.
func (s *InputService4TestShapeInputService4TestCaseOperation2Input) SetScalarArg(v string) *InputService4TestShapeInputService4TestCaseOperation2Input {
	s.ScalarArg = &v
	return s
}

type InputService4TestShapeInputService4TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

// InputService5ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService5ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService5ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService5ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService5ProtocolTest client from just a session.
//     svc := inputservice5protocoltest.New(mySession)
//
//     // Create a InputService5ProtocolTest client with additional configuration
//     svc := inputservice5protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService5ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService5ProtocolTest {
	c := p.ClientConfig("inputservice5protocoltest", cfgs...)
	return newInputService5ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService5ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService5ProtocolTest {
	svc := &InputService5ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService5ProtocolTest",
				ServiceID:     "InputService5ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService5ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService5ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService5TestCaseOperation1 = "OperationName"

// InputService5TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService5TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService5TestCaseOperation1 for more information on using the InputService5TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService5TestCaseOperation1Request method.
//    req, resp := client.InputService5TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService5ProtocolTest) InputService5TestCaseOperation1Request(input *InputService5TestShapeInputService5TestCaseOperation1Input) (req *request.Request, output *InputService5TestShapeInputService5TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService5TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService5TestShapeInputService5TestCaseOperation1Input{}
	}

	output = &InputService5TestShapeInputService5TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService5TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService5TestCaseOperation1 for usage and error information.
func (c *InputService5ProtocolTest) InputService5TestCaseOperation1(input *InputService5TestShapeInputService5TestCaseOperation1Input) (*InputService5TestShapeInputService5TestCaseOperation1Output, error) {
	req, out := c.InputService5TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService5TestCaseOperation1WithContext is the same as InputService5TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService5TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService5ProtocolTest) InputService5TestCaseOperation1WithContext(ctx aws.Context, input *InputService5TestShapeInputService5TestCaseOperation1Input, opts ...request.Option) (*InputService5TestShapeInputService5TestCaseOperation1Output, error) {
	req, out := c.InputService5TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService5TestShapeInputService5TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	MapArg map[string]*string `type:"map" flattened:"true"`
}

// SetMapArg sets the MapArg field's value.
func (s *InputService5TestShapeInputService5TestCaseOperation1Input) SetMapArg(v map[string]*string) *InputService5TestShapeInputService5TestCaseOperation1Input {
	s.MapArg = v
	return s
}

type InputService5TestShapeInputService5TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService6ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService6ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService6ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService6ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService6ProtocolTest client from just a session.
//     svc := inputservice6protocoltest.New(mySession)
//
//     // Create a InputService6ProtocolTest client with additional configuration
//     svc := inputservice6protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService6ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService6ProtocolTest {
	c := p.ClientConfig("inputservice6protocoltest", cfgs...)
	return newInputService6ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService6ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService6ProtocolTest {
	svc := &InputService6ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService6ProtocolTest",
				ServiceID:     "InputService6ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService6ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService6ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService6TestCaseOperation1 = "OperationName"

// InputService6TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService6TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService6TestCaseOperation1 for more information on using the InputService6TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService6TestCaseOperation1Request method.
//    req, resp := client.InputService6TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService6ProtocolTest) InputService6TestCaseOperation1Request(input *InputService6TestShapeInputService6TestCaseOperation1Input) (req *request.Request, output *InputService6TestShapeInputService6TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService6TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService6TestShapeInputService6TestCaseOperation1Input{}
	}

	output = &InputService6TestShapeInputService6TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService6TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService6TestCaseOperation1 for usage and error information.
func (c *InputService6ProtocolTest) InputService6TestCaseOperation1(input *InputService6TestShapeInputService6TestCaseOperation1Input) (*InputService6TestShapeInputService6TestCaseOperation1Output, error) {
	req, out := c.InputService6TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService6TestCaseOperation1WithContext is the same as InputService6TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService6TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService6ProtocolTest) InputService6TestCaseOperation1WithContext(ctx aws.Context, input *InputService6TestShapeInputService6TestCaseOperation1Input, opts ...request.Option) (*InputService6TestShapeInputService6TestCaseOperation1Output, error) {
	req, out := c.InputService6TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService6TestShapeInputService6TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	ListArg []*string `locationNameList:"item" type:"list"`
}

// SetListArg sets the ListArg field's value.
func (s *InputService6TestShapeInputService6TestCaseOperation1Input) SetListArg(v []*string) *InputService6TestShapeInputService6TestCaseOperation1Input {
	s.ListArg = v
	return s
}

type InputService6TestShapeInputService6TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService7ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService7ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService7ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService7ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService7ProtocolTest client from just a session.
//     svc := inputservice7protocoltest.New(mySession)
//
//     // Create a InputService7ProtocolTest client with additional configuration
//     svc := inputservice7protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService7ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService7ProtocolTest {
	c := p.ClientConfig("inputservice7protocoltest", cfgs...)
	return newInputService7ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService7ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService7ProtocolTest {
	svc := &InputService7ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService7ProtocolTest",
				ServiceID:     "InputService7ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService7ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService7ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService7TestCaseOperation1 = "OperationName"

// InputService7TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService7TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService7TestCaseOperation1 for more information on using the InputService7TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService7TestCaseOperation1Request method.
//    req, resp := client.InputService7TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService7ProtocolTest) InputService7TestCaseOperation1Request(input *InputService7TestShapeInputService7TestCaseOperation1Input) (req *request.Request, output *InputService7TestShapeInputService7TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService7TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService7TestShapeInputService7TestCaseOperation1Input{}
	}

	output = &InputService7TestShapeInputService7TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService7TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService7TestCaseOperation1 for usage and error information.
func (c *InputService7ProtocolTest) InputService7TestCaseOperation1(input *InputService7TestShapeInputService7TestCaseOperation1Input) (*InputService7TestShapeInputService7TestCaseOperation1Output, error) {
	req, out := c.InputService7TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService7TestCaseOperation1WithContext is the same as InputService7TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService7TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService7ProtocolTest) InputService7TestCaseOperation1WithContext(ctx aws.Context, input *InputService7TestShapeInputService7TestCaseOperation1Input, opts ...request.Option) (*InputService7TestShapeInputService7TestCaseOperation1Output, error) {
	req, out := c.InputService7TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService7TestShapeInputService7TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	ListArg []*string `locationNameList:"ListArgLocation" type:"list" flattened:"true"`

	ScalarArg *string `type:"string"`
}

// SetListArg sets the ListArg field's value.
func (s *InputService7TestShapeInputService7TestCaseOperation1Input) SetListArg(v []*string) *InputService7TestShapeInputService7TestCaseOperation1Input {
	s.ListArg = v
	return s
}

// SetScalarArg sets the ScalarArg field's value.
func (s *InputService7TestShapeInputService7TestCaseOperation1Input) SetScalarArg(v string) *InputService7TestShapeInputService7TestCaseOperation1Input {
	s.ScalarArg = &v
	return s
}

type InputService7TestShapeInputService7TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService8ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService8ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService8ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService8ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService8ProtocolTest client from just a session.
//     svc := inputservice8protocoltest.New(mySession)
//
//     // Create a InputService8ProtocolTest client with additional configuration
//     svc := inputservice8protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService8ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService8ProtocolTest {
	c := p.ClientConfig("inputservice8protocoltest", cfgs...)
	return newInputService8ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService8ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService8ProtocolTest {
	svc := &InputService8ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService8ProtocolTest",
				ServiceID:     "InputService8ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService8ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService8ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService8TestCaseOperation1 = "OperationName"

// InputService8TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService8TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService8TestCaseOperation1 for more information on using the InputService8TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService8TestCaseOperation1Request method.
//    req, resp := client.InputService8TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService8ProtocolTest) InputService8TestCaseOperation1Request(input *InputService8TestShapeInputService8TestCaseOperation1Input) (req *request.Request, output *InputService8TestShapeInputService8TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService8TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService8TestShapeInputService8TestCaseOperation1Input{}
	}

	output = &InputService8TestShapeInputService8TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService8TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService8TestCaseOperation1 for usage and error information.
func (c *InputService8ProtocolTest) InputService8TestCaseOperation1(input *InputService8TestShapeInputService8TestCaseOperation1Input) (*InputService8TestShapeInputService8TestCaseOperation1Output, error) {
	req, out := c.InputService8TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService8TestCaseOperation1WithContext is the same as InputService8TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService8TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService8ProtocolTest) InputService8TestCaseOperation1WithContext(ctx aws.Context, input *InputService8TestShapeInputService8TestCaseOperation1Input, opts ...request.Option) (*InputService8TestShapeInputService8TestCaseOperation1Output, error) {
	req, out := c.InputService8TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService8TestShapeInputService8TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	MapArg map[string]*string `type:"map"`
}

// SetMapArg sets the MapArg field's value.
func (s *InputService8TestShapeInputService8TestCaseOperation1Input) SetMapArg(v map[string]*string) *InputService8TestShapeInputService8TestCaseOperation1Input {
	s.MapArg = v
	return s
}

type InputService8TestShapeInputService8TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService9ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService9ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService9ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService9ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService9ProtocolTest client from just a session.
//     svc := inputservice9protocoltest.New(mySession)
//
//     // Create a InputService9ProtocolTest client with additional configuration
//     svc := inputservice9protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService9ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService9ProtocolTest {
	c := p.ClientConfig("inputservice9protocoltest", cfgs...)
	return newInputService9ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService9ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService9ProtocolTest {
	svc := &InputService9ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService9ProtocolTest",
				ServiceID:     "InputService9ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService9ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService9ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService9TestCaseOperation1 = "OperationName"

// InputService9TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService9TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService9TestCaseOperation1 for more information on using the InputService9TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService9TestCaseOperation1Request method.
//    req, resp := client.InputService9TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService9ProtocolTest) InputService9TestCaseOperation1Request(input *InputService9TestShapeInputService9TestCaseOperation1Input) (req *request.Request, output *InputService9TestShapeInputService9TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService9TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService9TestShapeInputService9TestCaseOperation1Input{}
	}

	output = &InputService9TestShapeInputService9TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService9TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService9TestCaseOperation1 for usage and error information.
func (c *InputService9ProtocolTest) InputService9TestCaseOperation1(input *InputService9TestShapeInputService9TestCaseOperation1Input) (*InputService9TestShapeInputService9TestCaseOperation1Output, error) {
	req, out := c.InputService9TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService9TestCaseOperation1WithContext is the same as InputService9TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService9TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService9ProtocolTest) InputService9TestCaseOperation1WithContext(ctx aws.Context, input *InputService9TestShapeInputService9TestCaseOperation1Input, opts ...request.Option) (*InputService9TestShapeInputService9TestCaseOperation1Output, error) {
	req, out := c.InputService9TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService9TestShapeInputService9TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	MapArg map[string]*string `locationNameKey:"TheKey" locationNameValue:"TheValue" type:"map"`
}

// SetMapArg sets the MapArg field's value.
func (s *InputService9TestShapeInputService9TestCaseOperation1Input) SetMapArg(v map[string]*string) *InputService9TestShapeInputService9TestCaseOperation1Input {
	s.MapArg = v
	return s
}

type InputService9TestShapeInputService9TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService10ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService10ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService10ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService10ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService10ProtocolTest client from just a session.
//     svc := inputservice10protocoltest.New(mySession)
//
//     // Create a InputService10ProtocolTest client with additional configuration
//     svc := inputservice10protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService10ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService10ProtocolTest {
	c := p.ClientConfig("inputservice10protocoltest", cfgs...)
	return newInputService10ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService10ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService10ProtocolTest {
	svc := &InputService10ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService10ProtocolTest",
				ServiceID:     "InputService10ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService10ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService10ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService10TestCaseOperation1 = "OperationName"

// InputService10TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService10TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService10TestCaseOperation1 for more information on using the InputService10TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService10TestCaseOperation1Request method.
//    req, resp := client.InputService10TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService10ProtocolTest) InputService10TestCaseOperation1Request(input *InputService10TestShapeInputService10TestCaseOperation1Input) (req *request.Request, output *InputService10TestShapeInputService10TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService10TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService10TestShapeInputService10TestCaseOperation1Input{}
	}

	output = &InputService10TestShapeInputService10TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService10TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService10TestCaseOperation1 for usage and error information.
func (c *InputService10ProtocolTest) InputService10TestCaseOperation1(input *InputService10TestShapeInputService10TestCaseOperation1Input) (*InputService10TestShapeInputService10TestCaseOperation1Output, error) {
	req, out := c.InputService10TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService10TestCaseOperation1WithContext is the same as InputService10TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService10TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService10ProtocolTest) InputService10TestCaseOperation1WithContext(ctx aws.Context, input *InputService10TestShapeInputService10TestCaseOperation1Input, opts ...request.Option) (*InputService10TestShapeInputService10TestCaseOperation1Output, error) {
	req, out := c.InputService10TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService10TestShapeInputService10TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	// BlobArg is automatically base64 encoded/decoded by the SDK.
	BlobArg []byte `type:"blob"`
}

// SetBlobArg sets the BlobArg field's value.
func (s *InputService10TestShapeInputService10TestCaseOperation1Input) SetBlobArg(v []byte) *InputService10TestShapeInputService10TestCaseOperation1Input {
	s.BlobArg = v
	return s
}

type InputService10TestShapeInputService10TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService11ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService11ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService11ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService11ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService11ProtocolTest client from just a session.
//     svc := inputservice11protocoltest.New(mySession)
//
//     // Create a InputService11ProtocolTest client with additional configuration
//     svc := inputservice11protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService11ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService11ProtocolTest {
	c := p.ClientConfig("inputservice11protocoltest", cfgs...)
	return newInputService11ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService11ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService11ProtocolTest {
	svc := &InputService11ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService11ProtocolTest",
				ServiceID:     "InputService11ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService11ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService11ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService11TestCaseOperation1 = "OperationName"

// InputService11TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService11TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService11TestCaseOperation1 for more information on using the InputService11TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService11TestCaseOperation1Request method.
//    req, resp := client.InputService11TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService11ProtocolTest) InputService11TestCaseOperation1Request(input *InputService11TestShapeInputService11TestCaseOperation1Input) (req *request.Request, output *InputService11TestShapeInputService11TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService11TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService11TestShapeInputService11TestCaseOperation1Input{}
	}

	output = &InputService11TestShapeInputService11TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService11TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService11TestCaseOperation1 for usage and error information.
func (c *InputService11ProtocolTest) InputService11TestCaseOperation1(input *InputService11TestShapeInputService11TestCaseOperation1Input) (*InputService11TestShapeInputService11TestCaseOperation1Output, error) {
	req, out := c.InputService11TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService11TestCaseOperation1WithContext is the same as InputService11TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService11TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService11ProtocolTest) InputService11TestCaseOperation1WithContext(ctx aws.Context, input *InputService11TestShapeInputService11TestCaseOperation1Input, opts ...request.Option) (*InputService11TestShapeInputService11TestCaseOperation1Output, error) {
	req, out := c.InputService11TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService11TestShapeInputService11TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	BlobArgs [][]byte `type:"list" flattened:"true"`
}

// SetBlobArgs sets the BlobArgs field's value.
func (s *InputService11TestShapeInputService11TestCaseOperation1Input) SetBlobArgs(v [][]byte) *InputService11TestShapeInputService11TestCaseOperation1Input {
	s.BlobArgs = v
	return s
}

type InputService11TestShapeInputService11TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService12ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService12ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService12ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService12ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService12ProtocolTest client from just a session.
//     svc := inputservice12protocoltest.New(mySession)
//
//     // Create a InputService12ProtocolTest client with additional configuration
//     svc := inputservice12protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService12ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService12ProtocolTest {
	c := p.ClientConfig("inputservice12protocoltest", cfgs...)
	return newInputService12ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService12ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService12ProtocolTest {
	svc := &InputService12ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService12ProtocolTest",
				ServiceID:     "InputService12ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService12ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService12ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService12TestCaseOperation1 = "OperationName"

// InputService12TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService12TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService12TestCaseOperation1 for more information on using the InputService12TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService12TestCaseOperation1Request method.
//    req, resp := client.InputService12TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService12ProtocolTest) InputService12TestCaseOperation1Request(input *InputService12TestShapeInputService12TestCaseOperation1Input) (req *request.Request, output *InputService12TestShapeInputService12TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService12TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService12TestShapeInputService12TestCaseOperation1Input{}
	}

	output = &InputService12TestShapeInputService12TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService12TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService12TestCaseOperation1 for usage and error information.
func (c *InputService12ProtocolTest) InputService12TestCaseOperation1(input *InputService12TestShapeInputService12TestCaseOperation1Input) (*InputService12TestShapeInputService12TestCaseOperation1Output, error) {
	req, out := c.InputService12TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService12TestCaseOperation1WithContext is the same as InputService12TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService12TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService12ProtocolTest) InputService12TestCaseOperation1WithContext(ctx aws.Context, input *InputService12TestShapeInputService12TestCaseOperation1Input, opts ...request.Option) (*InputService12TestShapeInputService12TestCaseOperation1Output, error) {
	req, out := c.InputService12TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService12TestShapeInputService12TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	TimeArg *time.Time `type:"timestamp"`

	TimeCustom *time.Time `type:"timestamp" timestampFormat:"unixTimestamp"`

	TimeFormat *time.Time `type:"timestamp" timestampFormat:"unixTimestamp"`
}

// SetTimeArg sets the TimeArg field's value.
func (s *InputService12TestShapeInputService12TestCaseOperation1Input) SetTimeArg(v time.Time) *InputService12TestShapeInputService12TestCaseOperation1Input {
	s.TimeArg = &v
	return s
}

// SetTimeCustom sets the TimeCustom field's value.
func (s *InputService12TestShapeInputService12TestCaseOperation1Input) SetTimeCustom(v time.Time) *InputService12TestShapeInputService12TestCaseOperation1Input {
	s.TimeCustom = &v
	return s
}

// SetTimeFormat sets the TimeFormat field's value.
func (s *InputService12TestShapeInputService12TestCaseOperation1Input) SetTimeFormat(v time.Time) *InputService12TestShapeInputService12TestCaseOperation1Input {
	s.TimeFormat = &v
	return s
}

type InputService12TestShapeInputService12TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService13ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService13ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService13ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService13ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService13ProtocolTest client from just a session.
//     svc := inputservice13protocoltest.New(mySession)
//
//     // Create a InputService13ProtocolTest client with additional configuration
//     svc := inputservice13protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService13ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService13ProtocolTest {
	c := p.ClientConfig("inputservice13protocoltest", cfgs...)
	return newInputService13ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService13ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService13ProtocolTest {
	svc := &InputService13ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService13ProtocolTest",
				ServiceID:     "InputService13ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService13ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService13ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService13TestCaseOperation1 = "OperationName"

// InputService13TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService13TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService13TestCaseOperation1 for more information on using the InputService13TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService13TestCaseOperation1Request method.
//    req, resp := client.InputService13TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService13ProtocolTest) InputService13TestCaseOperation1Request(input *InputService13TestShapeInputService13TestCaseOperation1Input) (req *request.Request, output *InputService13TestShapeInputService13TestCaseOperation1Output) {
	op := &request.Operation{
		Name:     opInputService13TestCaseOperation1,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService13TestShapeInputService13TestCaseOperation1Input{}
	}

	output = &InputService13TestShapeInputService13TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService13TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService13TestCaseOperation1 for usage and error information.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation1(input *InputService13TestShapeInputService13TestCaseOperation1Input) (*InputService13TestShapeInputService13TestCaseOperation1Output, error) {
	req, out := c.InputService13TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService13TestCaseOperation1WithContext is the same as InputService13TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService13TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation1WithContext(ctx aws.Context, input *InputService13TestShapeInputService13TestCaseOperation1Input, opts ...request.Option) (*InputService13TestShapeInputService13TestCaseOperation1Output, error) {
	req, out := c.InputService13TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService13TestCaseOperation2 = "OperationName"

// InputService13TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService13TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService13TestCaseOperation2 for more information on using the InputService13TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService13TestCaseOperation2Request method.
//    req, resp := client.InputService13TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService13ProtocolTest) InputService13TestCaseOperation2Request(input *InputService13TestShapeInputService13TestCaseOperation2Input) (req *request.Request, output *InputService13TestShapeInputService13TestCaseOperation2Output) {
	op := &request.Operation{
		Name:     opInputService13TestCaseOperation2,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService13TestShapeInputService13TestCaseOperation2Input{}
	}

	output = &InputService13TestShapeInputService13TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService13TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService13TestCaseOperation2 for usage and error information.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation2(input *InputService13TestShapeInputService13TestCaseOperation2Input) (*InputService13TestShapeInputService13TestCaseOperation2Output, error) {
	req, out := c.InputService13TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService13TestCaseOperation2WithContext is the same as InputService13TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService13TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation2WithContext(ctx aws.Context, input *InputService13TestShapeInputService13TestCaseOperation2Input, opts ...request.Option) (*InputService13TestShapeInputService13TestCaseOperation2Output, error) {
	req, out := c.InputService13TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService13TestCaseOperation3 = "OperationName"

// InputService13TestCaseOperation3Request generates a "aws/request.Request" representing the
// client's request for the InputService13TestCaseOperation3 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService13TestCaseOperation3 for more information on using the InputService13TestCaseOperation3
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService13TestCaseOperation3Request method.
//    req, resp := client.InputService13TestCaseOperation3Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService13ProtocolTest) InputService13TestCaseOperation3Request(input *InputService13TestShapeInputService13TestCaseOperation3Input) (req *request.Request, output *InputService13TestShapeInputService13TestCaseOperation3Output) {
	op := &request.Operation{
		Name:     opInputService13TestCaseOperation3,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService13TestShapeInputService13TestCaseOperation3Input{}
	}

	output = &InputService13TestShapeInputService13TestCaseOperation3Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService13TestCaseOperation3 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService13TestCaseOperation3 for usage and error information.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation3(input *InputService13TestShapeInputService13TestCaseOperation3Input) (*InputService13TestShapeInputService13TestCaseOperation3Output, error) {
	req, out := c.InputService13TestCaseOperation3Request(input)
	return out, req.Send()
}

// InputService13TestCaseOperation3WithContext is the same as InputService13TestCaseOperation3 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService13TestCaseOperation3 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation3WithContext(ctx aws.Context, input *InputService13TestShapeInputService13TestCaseOperation3Input, opts ...request.Option) (*InputService13TestShapeInputService13TestCaseOperation3Output, error) {
	req, out := c.InputService13TestCaseOperation3Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService13TestCaseOperation4 = "OperationName"

// InputService13TestCaseOperation4Request generates a "aws/request.Request" representing the
// client's request for the InputService13TestCaseOperation4 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService13TestCaseOperation4 for more information on using the InputService13TestCaseOperation4
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService13TestCaseOperation4Request method.
//    req, resp := client.InputService13TestCaseOperation4Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService13ProtocolTest) InputService13TestCaseOperation4Request(input *InputService13TestShapeInputService13TestCaseOperation4Input) (req *request.Request, output *InputService13TestShapeInputService13TestCaseOperation4Output) {
	op := &request.Operation{
		Name:     opInputService13TestCaseOperation4,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService13TestShapeInputService13TestCaseOperation4Input{}
	}

	output = &InputService13TestShapeInputService13TestCaseOperation4Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService13TestCaseOperation4 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService13TestCaseOperation4 for usage and error information.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation4(input *InputService13TestShapeInputService13TestCaseOperation4Input) (*InputService13TestShapeInputService13TestCaseOperation4Output, error) {
	req, out := c.InputService13TestCaseOperation4Request(input)
	return out, req.Send()
}

// InputService13TestCaseOperation4WithContext is the same as InputService13TestCaseOperation4 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService13TestCaseOperation4 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation4WithContext(ctx aws.Context, input *InputService13TestShapeInputService13TestCaseOperation4Input, opts ...request.Option) (*InputService13TestShapeInputService13TestCaseOperation4Output, error) {
	req, out := c.InputService13TestCaseOperation4Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService13TestCaseOperation5 = "OperationName"

// InputService13TestCaseOperation5Request generates a "aws/request.Request" representing the
// client's request for the InputService13TestCaseOperation5 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService13TestCaseOperation5 for more information on using the InputService13TestCaseOperation5
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService13TestCaseOperation5Request method.
//    req, resp := client.InputService13TestCaseOperation5Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService13ProtocolTest) InputService13TestCaseOperation5Request(input *InputService13TestShapeInputService13TestCaseOperation5Input) (req *request.Request, output *InputService13TestShapeInputService13TestCaseOperation5Output) {
	op := &request.Operation{
		Name:     opInputService13TestCaseOperation5,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService13TestShapeInputService13TestCaseOperation5Input{}
	}

	output = &InputService13TestShapeInputService13TestCaseOperation5Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService13TestCaseOperation5 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService13TestCaseOperation5 for usage and error information.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation5(input *InputService13TestShapeInputService13TestCaseOperation5Input) (*InputService13TestShapeInputService13TestCaseOperation5Output, error) {
	req, out := c.InputService13TestCaseOperation5Request(input)
	return out, req.Send()
}

// InputService13TestCaseOperation5WithContext is the same as InputService13TestCaseOperation5 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService13TestCaseOperation5 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation5WithContext(ctx aws.Context, input *InputService13TestShapeInputService13TestCaseOperation5Input, opts ...request.Option) (*InputService13TestShapeInputService13TestCaseOperation5Output, error) {
	req, out := c.InputService13TestCaseOperation5Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService13TestCaseOperation6 = "OperationName"

// InputService13TestCaseOperation6Request generates a "aws/request.Request" representing the
// client's request for the InputService13TestCaseOperation6 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService13TestCaseOperation6 for more information on using the InputService13TestCaseOperation6
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService13TestCaseOperation6Request method.
//    req, resp := client.InputService13TestCaseOperation6Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService13ProtocolTest) InputService13TestCaseOperation6Request(input *InputService13TestShapeInputService13TestCaseOperation6Input) (req *request.Request, output *InputService13TestShapeInputService13TestCaseOperation6Output) {
	op := &request.Operation{
		Name:     opInputService13TestCaseOperation6,
		HTTPPath: "/",
	}

	if input == nil {
		input = &InputService13TestShapeInputService13TestCaseOperation6Input{}
	}

	output = &InputService13TestShapeInputService13TestCaseOperation6Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService13TestCaseOperation6 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService13TestCaseOperation6 for usage and error information.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation6(input *InputService13TestShapeInputService13TestCaseOperation6Input) (*InputService13TestShapeInputService13TestCaseOperation6Output, error) {
	req, out := c.InputService13TestCaseOperation6Request(input)
	return out, req.Send()
}

// InputService13TestCaseOperation6WithContext is the same as InputService13TestCaseOperation6 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService13TestCaseOperation6 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation6WithContext(ctx aws.Context, input *InputService13TestShapeInputService13TestCaseOperation6Input, opts ...request.Option) (*InputService13TestShapeInputService13TestCaseOperation6Output, error) {
	req, out := c.InputService13TestCaseOperation6Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService13TestShapeInputService13TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	RecursiveStruct *InputService13TestShapeRecursiveStructType `type:"structure"`
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService13TestShapeInputService13TestCaseOperation1Input) SetRecursiveStruct(v *InputService13TestShapeRecursiveStructType) *InputService13TestShapeInputService13TestCaseOperation1Input {
	s.RecursiveStruct = v
	return s
}

type InputService13TestShapeInputService13TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService13TestShapeInputService13TestCaseOperation2Input struct {
	_ struct{} `type:"structure"`

	RecursiveStruct *InputService13TestShapeRecursiveStructType `type:"structure"`
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService13TestShapeInputService13TestCaseOperation2Input) SetRecursiveStruct(v *InputService13TestShapeRecursiveStructType) *InputService13TestShapeInputService13TestCaseOperation2Input {
	s.RecursiveStruct = v
	return s
}

type InputService13TestShapeInputService13TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

type InputService13TestShapeInputService13TestCaseOperation3Input struct {
	_ struct{} `type:"structure"`

	RecursiveStruct *InputService13TestShapeRecursiveStructType `type:"structure"`
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService13TestShapeInputService13TestCaseOperation3Input) SetRecursiveStruct(v *InputService13TestShapeRecursiveStructType) *InputService13TestShapeInputService13TestCaseOperation3Input {
	s.RecursiveStruct = v
	return s
}

type InputService13TestShapeInputService13TestCaseOperation3Output struct {
	_ struct{} `type:"structure"`
}

type InputService13TestShapeInputService13TestCaseOperation4Input struct {
	_ struct{} `type:"structure"`

	RecursiveStruct *InputService13TestShapeRecursiveStructType `type:"structure"`
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService13TestShapeInputService13TestCaseOperation4Input) SetRecursiveStruct(v *InputService13TestShapeRecursiveStructType) *InputService13TestShapeInputService13TestCaseOperation4Input {
	s.RecursiveStruct = v
	return s
}

type InputService13TestShapeInputService13TestCaseOperation4Output struct {
	_ struct{} `type:"structure"`
}

type InputService13TestShapeInputService13TestCaseOperation5Input struct {
	_ struct{} `type:"structure"`

	RecursiveStruct *InputService13TestShapeRecursiveStructType `type:"structure"`
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService13TestShapeInputService13TestCaseOperation5Input) SetRecursiveStruct(v *InputService13TestShapeRecursiveStructType) *InputService13TestShapeInputService13TestCaseOperation5Input {
	s.RecursiveStruct = v
	return s
}

type InputService13TestShapeInputService13TestCaseOperation5Output struct {
	_ struct{} `type:"structure"`
}

type InputService13TestShapeInputService13TestCaseOperation6Input struct {
	_ struct{} `type:"structure"`

	RecursiveStruct *InputService13TestShapeRecursiveStructType `type:"structure"`
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService13TestShapeInputService13TestCaseOperation6Input) SetRecursiveStruct(v *InputService13TestShapeRecursiveStructType) *InputService13TestShapeInputService13TestCaseOperation6Input {
	s.RecursiveStruct = v
	return s
}

type InputService13TestShapeInputService13TestCaseOperation6Output struct {
	_ struct{} `type:"structure"`
}

type InputService13TestShapeRecursiveStructType struct {
	_ struct{} `type:"structure"`

	NoRecurse *string `type:"string"`

	RecursiveList []*InputService13TestShapeRecursiveStructType `type:"list"`

	RecursiveMap map[string]*InputService13TestShapeRecursiveStructType `type:"map"`

	RecursiveStruct *InputService13TestShapeRecursiveStructType `type:"structure"`
}

// SetNoRecurse sets the NoRecurse field's value.
func (s *InputService13TestShapeRecursiveStructType) SetNoRecurse(v string) *InputService13TestShapeRecursiveStructType {
	s.NoRecurse = &v
	return s
}

// SetRecursiveList sets the RecursiveList field's value.
func (s *InputService13TestShapeRecursiveStructType) SetRecursiveList(v []*InputService13TestShapeRecursiveStructType) *InputService13TestShapeRecursiveStructType {
	s.RecursiveList = v
	return s
}

// SetRecursiveMap sets the RecursiveMap field's value.
func (s *InputService13TestShapeRecursiveStructType) SetRecursiveMap(v map[string]*InputService13TestShapeRecursiveStructType) *InputService13TestShapeRecursiveStructType {
	s.RecursiveMap = v
	return s
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService13TestShapeRecursiveStructType) SetRecursiveStruct(v *InputService13TestShapeRecursiveStructType) *InputService13TestShapeRecursiveStructType {
	s.RecursiveStruct = v
	return s
}

// InputService14ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService14ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService14ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService14ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService14ProtocolTest client from just a session.
//     svc := inputservice14protocoltest.New(mySession)
//
//     // Create a InputService14ProtocolTest client with additional configuration
//     svc := inputservice14protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService14ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService14ProtocolTest {
	c := p.ClientConfig("inputservice14protocoltest", cfgs...)
	return newInputService14ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService14ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService14ProtocolTest {
	svc := &InputService14ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService14ProtocolTest",
				ServiceID:     "InputService14ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService14ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService14ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService14TestCaseOperation1 = "OperationName"

// InputService14TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService14TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService14TestCaseOperation1 for more information on using the InputService14TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService14TestCaseOperation1Request method.
//    req, resp := client.InputService14TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService14ProtocolTest) InputService14TestCaseOperation1Request(input *InputService14TestShapeInputService14TestCaseOperation1Input) (req *request.Request, output *InputService14TestShapeInputService14TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService14TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService14TestShapeInputService14TestCaseOperation1Input{}
	}

	output = &InputService14TestShapeInputService14TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService14TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService14TestCaseOperation1 for usage and error information.
func (c *InputService14ProtocolTest) InputService14TestCaseOperation1(input *InputService14TestShapeInputService14TestCaseOperation1Input) (*InputService14TestShapeInputService14TestCaseOperation1Output, error) {
	req, out := c.InputService14TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService14TestCaseOperation1WithContext is the same as InputService14TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService14TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService14ProtocolTest) InputService14TestCaseOperation1WithContext(ctx aws.Context, input *InputService14TestShapeInputService14TestCaseOperation1Input, opts ...request.Option) (*InputService14TestShapeInputService14TestCaseOperation1Output, error) {
	req, out := c.InputService14TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService14TestCaseOperation2 = "OperationName"

// InputService14TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService14TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService14TestCaseOperation2 for more information on using the InputService14TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService14TestCaseOperation2Request method.
//    req, resp := client.InputService14TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService14ProtocolTest) InputService14TestCaseOperation2Request(input *InputService14TestShapeInputService14TestCaseOperation2Input) (req *request.Request, output *InputService14TestShapeInputService14TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService14TestCaseOperation2,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService14TestShapeInputService14TestCaseOperation2Input{}
	}

	output = &InputService14TestShapeInputService14TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService14TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService14TestCaseOperation2 for usage and error information.
func (c *InputService14ProtocolTest) InputService14TestCaseOperation2(input *InputService14TestShapeInputService14TestCaseOperation2Input) (*InputService14TestShapeInputService14TestCaseOperation2Output, error) {
	req, out := c.InputService14TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService14TestCaseOperation2WithContext is the same as InputService14TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService14TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService14ProtocolTest) InputService14TestCaseOperation2WithContext(ctx aws.Context, input *InputService14TestShapeInputService14TestCaseOperation2Input, opts ...request.Option) (*InputService14TestShapeInputService14TestCaseOperation2Output, error) {
	req, out := c.InputService14TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService14TestShapeInputService14TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	Token *string `type:"string" idempotencyToken:"true"`
}

// SetToken sets the Token field's value.
func (s *InputService14TestShapeInputService14TestCaseOperation1Input) SetToken(v string) *InputService14TestShapeInputService14TestCaseOperation1Input {
	s.Token = &v
	return s
}

type InputService14TestShapeInputService14TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService14TestShapeInputService14TestCaseOperation2Input struct {
	_ struct{} `type:"structure"`

	Token *string `type:"string" idempotencyToken:"true"`
}

// SetToken sets the Token field's value.
func (s *InputService14TestShapeInputService14TestCaseOperation2Input) SetToken(v string) *InputService14TestShapeInputService14TestCaseOperation2Input {
	s.Token = &v
	return s
}

type InputService14TestShapeInputService14TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

// InputService15ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService15ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService15ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService15ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService15ProtocolTest client from just a session.
//     svc := inputservice15protocoltest.New(mySession)
//
//     // Create a InputService15ProtocolTest client with additional configuration
//     svc := inputservice15protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService15ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService15ProtocolTest {
	c := p.ClientConfig("inputservice15protocoltest", cfgs...)
	return newInputService15ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService15ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService15ProtocolTest {
	svc := &InputService15ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService15ProtocolTest",
				ServiceID:     "InputService15ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService15ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService15ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService15TestCaseOperation1 = "OperationName"

// InputService15TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService15TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService15TestCaseOperation1 for more information on using the InputService15TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService15TestCaseOperation1Request method.
//    req, resp := client.InputService15TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService15ProtocolTest) InputService15TestCaseOperation1Request(input *InputService15TestShapeInputService15TestCaseOperation1Input) (req *request.Request, output *InputService15TestShapeInputService15TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService15TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService15TestShapeInputService15TestCaseOperation1Input{}
	}

	output = &InputService15TestShapeInputService15TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService15TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService15TestCaseOperation1 for usage and error information.
func (c *InputService15ProtocolTest) InputService15TestCaseOperation1(input *InputService15TestShapeInputService15TestCaseOperation1Input) (*InputService15TestShapeInputService15TestCaseOperation1Output, error) {
	req, out := c.InputService15TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService15TestCaseOperation1WithContext is the same as InputService15TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService15TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService15ProtocolTest) InputService15TestCaseOperation1WithContext(ctx aws.Context, input *InputService15TestShapeInputService15TestCaseOperation1Input, opts ...request.Option) (*InputService15TestShapeInputService15TestCaseOperation1Output, error) {
	req, out := c.InputService15TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService15TestCaseOperation2 = "OperationName"

// InputService15TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService15TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService15TestCaseOperation2 for more information on using the InputService15TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService15TestCaseOperation2Request method.
//    req, resp := client.InputService15TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService15ProtocolTest) InputService15TestCaseOperation2Request(input *InputService15TestShapeInputService15TestCaseOperation2Input) (req *request.Request, output *InputService15TestShapeInputService15TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService15TestCaseOperation2,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService15TestShapeInputService15TestCaseOperation2Input{}
	}

	output = &InputService15TestShapeInputService15TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService15TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService15TestCaseOperation2 for usage and error information.
func (c *InputService15ProtocolTest) InputService15TestCaseOperation2(input *InputService15TestShapeInputService15TestCaseOperation2Input) (*InputService15TestShapeInputService15TestCaseOperation2Output, error) {
	req, out := c.InputService15TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService15TestCaseOperation2WithContext is the same as InputService15TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService15TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService15ProtocolTest) InputService15TestCaseOperation2WithContext(ctx aws.Context, input *InputService15TestShapeInputService15TestCaseOperation2Input, opts ...request.Option) (*InputService15TestShapeInputService15TestCaseOperation2Output, error) {
	req, out := c.InputService15TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService15TestCaseOperation3 = "OperationName"

// InputService15TestCaseOperation3Request generates a "aws/request.Request" representing the
// client's request for the InputService15TestCaseOperation3 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService15TestCaseOperation3 for more information on using the InputService15TestCaseOperation3
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService15TestCaseOperation3Request method.
//    req, resp := client.InputService15TestCaseOperation3Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService15ProtocolTest) InputService15TestCaseOperation3Request(input *InputService15TestShapeInputService15TestCaseOperation3Input) (req *request.Request, output *InputService15TestShapeInputService15TestCaseOperation3Output) {
	op := &request.Operation{
		Name:       opInputService15TestCaseOperation3,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService15TestShapeInputService15TestCaseOperation3Input{}
	}

	output = &InputService15TestShapeInputService15TestCaseOperation3Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService15TestCaseOperation3 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService15TestCaseOperation3 for usage and error information.
func (c *InputService15ProtocolTest) InputService15TestCaseOperation3(input *InputService15TestShapeInputService15TestCaseOperation3Input) (*InputService15TestShapeInputService15TestCaseOperation3Output, error) {
	req, out := c.InputService15TestCaseOperation3Request(input)
	return out, req.Send()
}

// InputService15TestCaseOperation3WithContext is the same as InputService15TestCaseOperation3 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService15TestCaseOperation3 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService15ProtocolTest) InputService15TestCaseOperation3WithContext(ctx aws.Context, input *InputService15TestShapeInputService15TestCaseOperation3Input, opts ...request.Option) (*InputService15TestShapeInputService15TestCaseOperation3Output, error) {
	req, out := c.InputService15TestCaseOperation3Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService15TestShapeInputService15TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	FooEnum *string `type:"string" enum:"InputService15TestShapeEnumType"`

	ListEnums []*string `type:"list"`
}

// SetFooEnum sets the FooEnum field's value.
func (s *InputService15TestShapeInputService15TestCaseOperation1Input) SetFooEnum(v string) *InputService15TestShapeInputService15TestCaseOperation1Input {
	s.FooEnum = &v
	return s
}

// SetListEnums sets the ListEnums field's value.
func (s *InputService15TestShapeInputService15TestCaseOperation1Input) SetListEnums(v []*string) *InputService15TestShapeInputService15TestCaseOperation1Input {
	s.ListEnums = v
	return s
}

type InputService15TestShapeInputService15TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService15TestShapeInputService15TestCaseOperation2Input struct {
	_ struct{} `type:"structure"`

	FooEnum *string `type:"string" enum:"InputService15TestShapeEnumType"`

	ListEnums []*string `type:"list"`
}

// SetFooEnum sets the FooEnum field's value.
func (s *InputService15TestShapeInputService15TestCaseOperation2Input) SetFooEnum(v string) *InputService15TestShapeInputService15TestCaseOperation2Input {
	s.FooEnum = &v
	return s
}

// SetListEnums sets the ListEnums field's value.
func (s *InputService15TestShapeInputService15TestCaseOperation2Input) SetListEnums(v []*string) *InputService15TestShapeInputService15TestCaseOperation2Input {
	s.ListEnums = v
	return s
}

type InputService15TestShapeInputService15TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

type InputService15TestShapeInputService15TestCaseOperation3Input struct {
	_ struct{} `type:"structure"`

	FooEnum *string `type:"string" enum:"InputService15TestShapeEnumType"`

	ListEnums []*string `type:"list"`
}

// SetFooEnum sets the FooEnum field's value.
func (s *InputService15TestShapeInputService15TestCaseOperation3Input) SetFooEnum(v string) *InputService15TestShapeInputService15TestCaseOperation3Input {
	s.FooEnum = &v
	return s
}

// SetListEnums sets the ListEnums field's value.
func (s *InputService15TestShapeInputService15TestCaseOperation3Input) SetListEnums(v []*string) *InputService15TestShapeInputService15TestCaseOperation3Input {
	s.ListEnums = v
	return s
}

type InputService15TestShapeInputService15TestCaseOperation3Output struct {
	_ struct{} `type:"structure"`
}

const (
	// EnumTypeFoo is a InputService15TestShapeEnumType enum value
	EnumTypeFoo = "foo"

	// EnumTypeBar is a InputService15TestShapeEnumType enum value
	EnumTypeBar = "bar"
)

// InputService16ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService16ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService16ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService16ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService16ProtocolTest client from just a session.
//     svc := inputservice16protocoltest.New(mySession)
//
//     // Create a InputService16ProtocolTest client with additional configuration
//     svc := inputservice16protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService16ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService16ProtocolTest {
	c := p.ClientConfig("inputservice16protocoltest", cfgs...)
	return newInputService16ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService16ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService16ProtocolTest {
	svc := &InputService16ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService16ProtocolTest",
				ServiceID:     "InputService16ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(query.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(query.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(query.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(query.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService16ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService16ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService16TestCaseOperation1 = "StaticOp"

// InputService16TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService16TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService16TestCaseOperation1 for more information on using the InputService16TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService16TestCaseOperation1Request method.
//    req, resp := client.InputService16TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService16ProtocolTest) InputService16TestCaseOperation1Request(input *InputService16TestShapeInputService16TestCaseOperation1Input) (req *request.Request, output *InputService16TestShapeInputService16TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService16TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService16TestShapeInputService16TestCaseOperation1Input{}
	}

	output = &InputService16TestShapeInputService16TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	req.Handlers.Build.PushBackNamed(protocol.NewHostPrefixHandler("data-", nil))
	req.Handlers.Build.PushBackNamed(protocol.ValidateEndpointHostHandler)
	return
}

// InputService16TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService16TestCaseOperation1 for usage and error information.
func (c *InputService16ProtocolTest) InputService16TestCaseOperation1(input *InputService16TestShapeInputService16TestCaseOperation1Input) (*InputService16TestShapeInputService16TestCaseOperation1Output, error) {
	req, out := c.InputService16TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService16TestCaseOperation1WithContext is the same as InputService16TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService16TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService16ProtocolTest) InputService16TestCaseOperation1WithContext(ctx aws.Context, input *InputService16TestShapeInputService16TestCaseOperation1Input, opts ...request.Option) (*InputService16TestShapeInputService16TestCaseOperation1Output, error) {
	req, out := c.InputService16TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService16TestCaseOperation2 = "MemberRefOp"

// InputService16TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService16TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService16TestCaseOperation2 for more information on using the InputService16TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService16TestCaseOperation2Request method.
//    req, resp := client.InputService16TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService16ProtocolTest) InputService16TestCaseOperation2Request(input *InputService16TestShapeInputService16TestCaseOperation2Input) (req *request.Request, output *InputService16TestShapeInputService16TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService16TestCaseOperation2,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService16TestShapeInputService16TestCaseOperation2Input{}
	}

	output = &InputService16TestShapeInputService16TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(query.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	req.Handlers.Build.PushBackNamed(protocol.NewHostPrefixHandler("foo-{Name}.", input.hostLabels))
	req.Handlers.Build.PushBackNamed(protocol.ValidateEndpointHostHandler)
	return
}

// InputService16TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService16TestCaseOperation2 for usage and error information.
func (c *InputService16ProtocolTest) InputService16TestCaseOperation2(input *InputService16TestShapeInputService16TestCaseOperation2Input) (*InputService16TestShapeInputService16TestCaseOperation2Output, error) {
	req, out := c.InputService16TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService16TestCaseOperation2WithContext is the same as InputService16TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService16TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService16ProtocolTest) InputService16TestCaseOperation2WithContext(ctx aws.Context, input *InputService16TestShapeInputService16TestCaseOperation2Input, opts ...request.Option) (*InputService16TestShapeInputService16TestCaseOperation2Output, error) {
	req, out := c.InputService16TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService16TestShapeInputService16TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	Name *string `type:"string"`
}

// SetName sets the Name field's value.
func (s *InputService16TestShapeInputService16TestCaseOperation1Input) SetName(v string) *InputService16TestShapeInputService16TestCaseOperation1Input {
	s.Name = &v
	return s
}

type InputService16TestShapeInputService16TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService16TestShapeInputService16TestCaseOperation2Input struct {
	_ struct{} `type:"structure"`

	// Name is a required field
	Name *string `type:"string" required:"true"`
}

// Validate inspects the fields of the type to determine if they are valid.
func (s *InputService16TestShapeInputService16TestCaseOperation2Input) Validate() error {
	invalidParams := request.ErrInvalidParams{Context: "InputService16TestShapeInputService16TestCaseOperation2Input"}
	if s.Name == nil {
		invalidParams.Add(request.NewErrParamRequired("Name"))
	}
	if s.Name != nil && len(*s.Name) < 1 {
		invalidParams.Add(request.NewErrParamMinLen("Name", 1))
	}

	if invalidParams.Len() > 0 {
		return invalidParams
	}
	return nil
}

// SetName sets the Name field's value.
func (s *InputService16TestShapeInputService16TestCaseOperation2Input) SetName(v string) *InputService16TestShapeInputService16TestCaseOperation2Input {
	s.Name = &v
	return s
}

func (s *InputService16TestShapeInputService16TestCaseOperation2Input) hostLabels() map[string]string {
	return map[string]string{
		"Name": aws.StringValue(s.Name),
	}
}

type InputService16TestShapeInputService16TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

//
// Tests begin here
//

func TestInputService1ProtocolTestScalarMembersCase1(t *testing.T) {
	svc := NewInputService1ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService1TestShapeInputService1TestCaseOperation1Input{
		Bar: aws.String("val2"),
		Foo: aws.String("val1"),
	}
	req, _ := svc.InputService1TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&Bar=val2&Foo=val1&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService1ProtocolTestScalarMembersCase2(t *testing.T) {
	svc := NewInputService1ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService1TestShapeInputService1TestCaseOperation2Input{
		Baz: aws.Bool(true),
	}
	req, _ := svc.InputService1TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&Baz=true&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService1ProtocolTestScalarMembersCase3(t *testing.T) {
	svc := NewInputService1ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService1TestShapeInputService1TestCaseOperation3Input{
		Baz: aws.Bool(false),
	}
	req, _ := svc.InputService1TestCaseOperation3Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&Baz=false&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService2ProtocolTestNestedStructureMembersCase1(t *testing.T) {
	svc := NewInputService2ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService2TestShapeInputService2TestCaseOperation1Input{
		StructArg: &InputService2TestShapeStructType{
			ScalarArg: aws.String("foo"),
		},
	}
	req, _ := svc.InputService2TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&StructArg.ScalarArg=foo&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService3ProtocolTestListTypesCase1(t *testing.T) {
	svc := NewInputService3ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService3TestShapeInputService3TestCaseOperation1Input{
		ListArg: []*string{
			aws.String("foo"),
			aws.String("bar"),
			aws.String("baz"),
		},
	}
	req, _ := svc.InputService3TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&ListArg.member.1=foo&ListArg.member.2=bar&ListArg.member.3=baz&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService3ProtocolTestListTypesCase2(t *testing.T) {
	svc := NewInputService3ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService3TestShapeInputService3TestCaseOperation2Input{
		ListArg: []*string{},
	}
	req, _ := svc.InputService3TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&ListArg=&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService4ProtocolTestFlattenedListCase1(t *testing.T) {
	svc := NewInputService4ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService4TestShapeInputService4TestCaseOperation1Input{
		ListArg: []*string{
			aws.String("a"),
			aws.String("b"),
			aws.String("c"),
		},
		ScalarArg: aws.String("foo"),
	}
	req, _ := svc.InputService4TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&ListArg.1=a&ListArg.2=b&ListArg.3=c&ScalarArg=foo&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService4ProtocolTestFlattenedListCase2(t *testing.T) {
	svc := NewInputService4ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService4TestShapeInputService4TestCaseOperation2Input{
		NamedListArg: []*string{
			aws.String("a"),
		},
	}
	req, _ := svc.InputService4TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&Foo.1=a&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService5ProtocolTestSerializeFlattenedMapTypeCase1(t *testing.T) {
	svc := NewInputService5ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService5TestShapeInputService5TestCaseOperation1Input{
		MapArg: map[string]*string{
			"key1": aws.String("val1"),
			"key2": aws.String("val2"),
		},
	}
	req, _ := svc.InputService5TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&MapArg.1.key=key1&MapArg.1.value=val1&MapArg.2.key=key2&MapArg.2.value=val2&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService6ProtocolTestNonFlattenedListWithLocationNameCase1(t *testing.T) {
	svc := NewInputService6ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService6TestShapeInputService6TestCaseOperation1Input{
		ListArg: []*string{
			aws.String("a"),
			aws.String("b"),
			aws.String("c"),
		},
	}
	req, _ := svc.InputService6TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&ListArg.item.1=a&ListArg.item.2=b&ListArg.item.3=c&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService7ProtocolTestFlattenedListWithLocationNameCase1(t *testing.T) {
	svc := NewInputService7ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService7TestShapeInputService7TestCaseOperation1Input{
		ListArg: []*string{
			aws.String("a"),
			aws.String("b"),
			aws.String("c"),
		},
		ScalarArg: aws.String("foo"),
	}
	req, _ := svc.InputService7TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&ListArgLocation.1=a&ListArgLocation.2=b&ListArgLocation.3=c&ScalarArg=foo&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService8ProtocolTestSerializeMapTypeCase1(t *testing.T) {
	svc := NewInputService8ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService8TestShapeInputService8TestCaseOperation1Input{
		MapArg: map[string]*string{
			"key1": aws.String("val1"),
			"key2": aws.String("val2"),
		},
	}
	req, _ := svc.InputService8TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&MapArg.entry.1.key=key1&MapArg.entry.1.value=val1&MapArg.entry.2.key=key2&MapArg.entry.2.value=val2&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService9ProtocolTestSerializeMapTypeWithLocationNameCase1(t *testing.T) {
	svc := NewInputService9ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService9TestShapeInputService9TestCaseOperation1Input{
		MapArg: map[string]*string{
			"key1": aws.String("val1"),
			"key2": aws.String("val2"),
		},
	}
	req, _ := svc.InputService9TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&MapArg.entry.1.TheKey=key1&MapArg.entry.1.TheValue=val1&MapArg.entry.2.TheKey=key2&MapArg.entry.2.TheValue=val2&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService10ProtocolTestBase64EncodedBlobsCase1(t *testing.T) {
	svc := NewInputService10ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService10TestShapeInputService10TestCaseOperation1Input{
		BlobArg: []byte("foo"),
	}
	req, _ := svc.InputService10TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&BlobArg=Zm9v&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService11ProtocolTestBase64EncodedBlobsNestedCase1(t *testing.T) {
	svc := NewInputService11ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService11TestShapeInputService11TestCaseOperation1Input{
		BlobArgs: [][]byte{
			[]byte("foo"),
		},
	}
	req, _ := svc.InputService11TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&BlobArgs.1=Zm9v&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService12ProtocolTestTimestampValuesCase1(t *testing.T) {
	svc := NewInputService12ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService12TestShapeInputService12TestCaseOperation1Input{
		TimeArg:    aws.Time(time.Unix(1422172800, 0)),
		TimeCustom: aws.Time(time.Unix(1422172800, 0)),
		TimeFormat: aws.Time(time.Unix(1422172800, 0)),
	}
	req, _ := svc.InputService12TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&TimeArg=2015-01-25T08%3A00%3A00Z&TimeCustom=1422172800&TimeFormat=1422172800&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService13ProtocolTestRecursiveShapesCase1(t *testing.T) {
	svc := NewInputService13ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService13TestShapeInputService13TestCaseOperation1Input{
		RecursiveStruct: &InputService13TestShapeRecursiveStructType{
			NoRecurse: aws.String("foo"),
		},
	}
	req, _ := svc.InputService13TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&RecursiveStruct.NoRecurse=foo&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService13ProtocolTestRecursiveShapesCase2(t *testing.T) {
	svc := NewInputService13ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService13TestShapeInputService13TestCaseOperation2Input{
		RecursiveStruct: &InputService13TestShapeRecursiveStructType{
			RecursiveStruct: &InputService13TestShapeRecursiveStructType{
				NoRecurse: aws.String("foo"),
			},
		},
	}
	req, _ := svc.InputService13TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&RecursiveStruct.RecursiveStruct.NoRecurse=foo&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService13ProtocolTestRecursiveShapesCase3(t *testing.T) {
	svc := NewInputService13ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService13TestShapeInputService13TestCaseOperation3Input{
		RecursiveStruct: &InputService13TestShapeRecursiveStructType{
			RecursiveStruct: &InputService13TestShapeRecursiveStructType{
				RecursiveStruct: &InputService13TestShapeRecursiveStructType{
					RecursiveStruct: &InputService13TestShapeRecursiveStructType{
						NoRecurse: aws.String("foo"),
					},
				},
			},
		},
	}
	req, _ := svc.InputService13TestCaseOperation3Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&RecursiveStruct.RecursiveStruct.RecursiveStruct.RecursiveStruct.NoRecurse=foo&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService13ProtocolTestRecursiveShapesCase4(t *testing.T) {
	svc := NewInputService13ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService13TestShapeInputService13TestCaseOperation4Input{
		RecursiveStruct: &InputService13TestShapeRecursiveStructType{
			RecursiveList: []*InputService13TestShapeRecursiveStructType{
				{
					NoRecurse: aws.String("foo"),
				},
				{
					NoRecurse: aws.String("bar"),
				},
			},
		},
	}
	req, _ := svc.InputService13TestCaseOperation4Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&RecursiveStruct.RecursiveList.member.1.NoRecurse=foo&RecursiveStruct.RecursiveList.member.2.NoRecurse=bar&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService13ProtocolTestRecursiveShapesCase5(t *testing.T) {
	svc := NewInputService13ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService13TestShapeInputService13TestCaseOperation5Input{
		RecursiveStruct: &InputService13TestShapeRecursiveStructType{
			RecursiveList: []*InputService13TestShapeRecursiveStructType{
				{
					NoRecurse: aws.String("foo"),
				},
				{
					RecursiveStruct: &InputService13TestShapeRecursiveStructType{
						NoRecurse: aws.String("bar"),
					},
				},
			},
		},
	}
	req, _ := svc.InputService13TestCaseOperation5Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&RecursiveStruct.RecursiveList.member.1.NoRecurse=foo&RecursiveStruct.RecursiveList.member.2.RecursiveStruct.NoRecurse=bar&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService13ProtocolTestRecursiveShapesCase6(t *testing.T) {
	svc := NewInputService13ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService13TestShapeInputService13TestCaseOperation6Input{
		RecursiveStruct: &InputService13TestShapeRecursiveStructType{
			RecursiveMap: map[string]*InputService13TestShapeRecursiveStructType{
				"bar": {
					NoRecurse: aws.String("bar"),
				},
				"foo": {
					NoRecurse: aws.String("foo"),
				},
			},
		},
	}
	req, _ := svc.InputService13TestCaseOperation6Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&RecursiveStruct.RecursiveMap.entry.1.key=foo&RecursiveStruct.RecursiveMap.entry.1.value.NoRecurse=foo&RecursiveStruct.RecursiveMap.entry.2.key=bar&RecursiveStruct.RecursiveMap.entry.2.value.NoRecurse=bar&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService14ProtocolTestIdempotencyTokenAutoFillCase1(t *testing.T) {
	svc := NewInputService14ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService14TestShapeInputService14TestCaseOperation1Input{
		Token: aws.String("abc123"),
	}
	req, _ := svc.InputService14TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&Token=abc123&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService14ProtocolTestIdempotencyTokenAutoFillCase2(t *testing.T) {
	svc := NewInputService14ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService14TestShapeInputService14TestCaseOperation2Input{}
	req, _ := svc.InputService14TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&Token=00000000-0000-4000-8000-000000000000&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService15ProtocolTestEnumCase1(t *testing.T) {
	svc := NewInputService15ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService15TestShapeInputService15TestCaseOperation1Input{
		FooEnum: aws.String("foo"),
		ListEnums: []*string{
			aws.String("foo"),
			aws.String(""),
			aws.String("bar"),
		},
	}
	req, _ := svc.InputService15TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&FooEnum=foo&ListEnums.member.1=foo&ListEnums.member.2=&ListEnums.member.3=bar&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService15ProtocolTestEnumCase2(t *testing.T) {
	svc := NewInputService15ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService15TestShapeInputService15TestCaseOperation2Input{
		FooEnum: aws.String("foo"),
	}
	req, _ := svc.InputService15TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&FooEnum=foo&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService15ProtocolTestEnumCase3(t *testing.T) {
	svc := NewInputService15ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService15TestShapeInputService15TestCaseOperation3Input{}
	req, _ := svc.InputService15TestCaseOperation3Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=OperationName&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService16ProtocolTestEndpointHostTraitCase1(t *testing.T) {
	svc := NewInputService16ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://service.region.amazonaws.com")})
	input := &InputService16TestShapeInputService16TestCaseOperation1Input{
		Name: aws.String("myname"),
	}
	req, _ := svc.InputService16TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=StaticOp&Name=myname&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://data-service.region.amazonaws.com/", r.URL.String())

	// assert headers

}

func TestInputService16ProtocolTestEndpointHostTraitCase2(t *testing.T) {
	svc := NewInputService16ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://service.region.amazonaws.com")})
	input := &InputService16TestShapeInputService16TestCaseOperation2Input{
		Name: aws.String("myname"),
	}
	req, _ := svc.InputService16TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body, _ := ioutil.ReadAll(r.Body)
	awstesting.AssertQuery(t, `Action=MemberRefOp&Name=myname&Version=2014-01-01`, util.Trim(string(body)))

	// assert URL
	awstesting.AssertURL(t, "https://foo-myname.service.region.amazonaws.com/", r.URL.String())

	// assert headers

}
