// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/aws/config.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package aws

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/pkg/errors"
)

// DynamoConfigFromURL returns AWS config from given URL. It expects escaped
// AWS Access key ID & Secret Access Key to be encoded in the URL. It
// also expects region specified as a host (letting AWS generate full
// endpoint) or fully valid endpoint with dummy region assumed (e.g
// for URLs to emulated services).
func DynamoConfigFromURL(awsURL *url.URL) (*dynamodb.Options, error) {
	httpClient := &http.Client{
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
	}
	config := dynamodb.Options{HTTPClient: httpClient}

	// Use a custom http.Client with the golang defaults but also specifying
	// MaxIdleConnsPerHost because of a bug in golang https://github.com/golang/go/issues/13801
	// where MaxIdleConnsPerHost does not work as expected.

	if awsURL.User != nil {
		username := awsURL.User.Username()
		password, _ := awsURL.User.Password()

		// We request at least the username or password being set to enable the static credentials.
		if username != "" || password != "" {
			config.Credentials = credentials.NewStaticCredentialsProvider(username, password, "")
		}
	}

	if strings.Contains(awsURL.Host, ".") {
		region := os.Getenv("AWS_REGION")
		if region == "" {
			region = "dummy"
		}
		config.Region = region
		if awsURL.Scheme == "https" {
			config.BaseEndpoint = aws.String(fmt.Sprintf("https://%s", awsURL.Host))
		}
		config.BaseEndpoint = aws.String(fmt.Sprintf("http://%s", awsURL.Host))
	} else {
		config.Region = awsURL.Host
	}
	// Let AWS generate default endpoint based on region passed as a host in URL.
	return &config, nil
}

func CredentialsFromURL(awsURL *url.URL) (key, secret, session string, err error) {
	if awsURL.User != nil {
		username := awsURL.User.Username()
		password, _ := awsURL.User.Password()

		// We request at least the username or password being set to enable the static credentials.
		if username != "" || password != "" {
			return username, password, "", nil
		}
	}
	return "", "", "", errors.New("Unable to build AWS credentials from URL")
}
