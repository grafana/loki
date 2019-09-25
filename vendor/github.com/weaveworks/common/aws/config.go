package aws

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
)

// ConfigFromURL returns AWS config from given URL. It expects escaped
// AWS Access key ID & Secret Access Key to be encoded in the URL. It
// also expects region specified as a host (letting AWS generate full
// endpoint) or fully valid endpoint with dummy region assumed (e.g
// for URLs to emulated services).
func ConfigFromURL(awsURL *url.URL) (*aws.Config, error) {
	config := aws.NewConfig().
		// Use a custom http.Client with the golang defaults but also specifying
		// MaxIdleConnsPerHost because of a bug in golang https://github.com/golang/go/issues/13801
		// where MaxIdleConnsPerHost does not work as expected.
		WithHTTPClient(&http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
					DualStack: true,
				}).DialContext,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				MaxIdleConnsPerHost:   100,
				TLSHandshakeTimeout:   3 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		})

	if awsURL.User != nil {
		password, _ := awsURL.User.Password()
		creds := credentials.NewStaticCredentials(awsURL.User.Username(), password, "")
		config = config.WithCredentials(creds)
	}

	if strings.Contains(awsURL.Host, ".") {
		return config.WithEndpoint(fmt.Sprintf("http://%s", awsURL.Host)).WithRegion("dummy"), nil
	}

	// Let AWS generate default endpoint based on region passed as a host in URL.
	return config.WithRegion(awsURL.Host), nil
}
