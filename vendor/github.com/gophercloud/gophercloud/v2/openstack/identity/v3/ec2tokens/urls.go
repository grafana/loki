package ec2tokens

import "github.com/gophercloud/gophercloud/v2"

func ec2tokensURL(c *gophercloud.ServiceClient) string {
	return c.ServiceURL("ec2tokens")
}

func s3tokensURL(c *gophercloud.ServiceClient) string {
	return c.ServiceURL("s3tokens")
}
