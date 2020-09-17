package aws

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
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
		username := awsURL.User.Username()
		password, _ := awsURL.User.Password()

		// We request at least the username or password being set to enable the static credentials.
		if username != "" || password != "" {
			config = config.WithCredentials(credentials.NewStaticCredentials(username, password, ""))
		}
	}

	if strings.Contains(awsURL.Host, ".") {
		region := os.Getenv("AWS_REGION")
		if region == "" {
			region = "dummy"
		}
		if awsURL.Scheme == "https" {
			return config.WithEndpoint(fmt.Sprintf("https://%s", awsURL.Host)).WithRegion(region), nil
		}
		return config.WithEndpoint(fmt.Sprintf("http://%s", awsURL.Host)).WithRegion(region), nil
	}

	// Let AWS generate default endpoint based on region passed as a host in URL.
	return config.WithRegion(awsURL.Host), nil
}
