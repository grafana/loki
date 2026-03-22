package aws

import (
	"net/url"
)

const InvalidAWSRegion = "dummy"

func credentialsFromURL(awsURL *url.URL) (key, secret string) {
	if awsURL.User != nil {
		username := awsURL.User.Username()
		password, _ := awsURL.User.Password()

		// We request at least the username or password being set to enable the static credentials.
		if username != "" || password != "" {
			return username, password
		}
	}
	// Return empty credentials instead of error to allow AWS SDK to use default credential chain
	// (environment variables, IAM roles, etc.)
	return "", ""
}
