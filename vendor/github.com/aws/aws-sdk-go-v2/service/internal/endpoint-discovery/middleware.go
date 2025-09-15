package endpointdiscovery

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/aws/smithy-go/logging"
	"github.com/aws/smithy-go/middleware"
)

// DiscoverEndpointOptions are optionals used with DiscoverEndpoint operation.
type DiscoverEndpointOptions struct {

	// EndpointResolverUsedForDiscovery is the endpoint resolver used to
	// resolve an endpoint for discovery api call.
	EndpointResolverUsedForDiscovery interface{}

	// DisableHTTPS will disable tls for endpoint discovery call and
	// subsequent discovered endpoint if service did not return an
	// endpoint scheme.
	DisableHTTPS bool

	// Logger to log warnings or debug statements.
	Logger logging.Logger
}

// DiscoverEndpoint is a finalize step middleware used to discover endpoint
// for an API operation.
type DiscoverEndpoint struct {

	// Options provides optional settings used with
	// Discover Endpoint operation.
	Options []func(*DiscoverEndpointOptions)

	// DiscoverOperation represents the endpoint discovery operation that
	// returns an Endpoint or error.
	DiscoverOperation func(ctx context.Context, region string, options ...func(*DiscoverEndpointOptions)) (WeightedAddress, error)

	// EndpointDiscoveryEnableState represents the customer configuration for endpoint
	// discovery feature.
	EndpointDiscoveryEnableState aws.EndpointDiscoveryEnableState

	// EndpointDiscoveryRequired states if an operation requires to perform
	// endpoint discovery.
	EndpointDiscoveryRequired bool

	// The client region
	Region string
}

// ID represents the middleware identifier
func (*DiscoverEndpoint) ID() string {
	return "DiscoverEndpoint"
}

// HandleFinalize performs endpoint discovery and updates the request host with
// the result.
//
// The resolved host from this procedure MUST override that of modeled endpoint
// resolution and middleware should be ordered accordingly.
func (d *DiscoverEndpoint) HandleFinalize(
	ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler,
) (out middleware.FinalizeOutput, metadata middleware.Metadata, err error) {
	if d.EndpointDiscoveryEnableState == aws.EndpointDiscoveryDisabled {
		return next.HandleFinalize(ctx, in)
	}

	if !d.EndpointDiscoveryRequired && d.EndpointDiscoveryEnableState != aws.EndpointDiscoveryEnabled {
		return next.HandleFinalize(ctx, in)
	}

	if es := awsmiddleware.GetEndpointSource(ctx); es == aws.EndpointSourceCustom {
		if d.EndpointDiscoveryEnableState == aws.EndpointDiscoveryEnabled {
			return middleware.FinalizeOutput{}, middleware.Metadata{},
				fmt.Errorf("Invalid configuration: endpoint discovery is enabled, but a custom endpoint is provided")
		}

		return next.HandleFinalize(ctx, in)
	}

	weightedAddress, err := d.DiscoverOperation(ctx, d.Region, d.Options...)
	if err != nil {
		return middleware.FinalizeOutput{}, middleware.Metadata{}, err
	}

	req, ok := in.Request.(*smithyhttp.Request)
	if !ok {
		return middleware.FinalizeOutput{}, middleware.Metadata{},
			fmt.Errorf("expected request to be of type *smithyhttp.Request, got %T", in.Request)
	}

	if weightedAddress.URL != nil {
		// we only want the host, normal endpoint resolution can include path/query
		req.URL.Host = weightedAddress.URL.Host
	}

	return next.HandleFinalize(ctx, in)
}
