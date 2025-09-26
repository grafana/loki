// Deprecated: aws-sdk-go is deprecated. Use aws-sdk-go-v2.
// See https://aws.amazon.com/blogs/developer/announcing-end-of-support-for-aws-sdk-for-go-v1-on-july-31-2025/.
package metadata

// ClientInfo wraps immutable data from the client.Client structure.
type ClientInfo struct {
	ServiceName    string
	ServiceID      string
	APIVersion     string
	PartitionID    string
	Endpoint       string
	SigningName    string
	SigningRegion  string
	JSONVersion    string
	TargetPrefix   string
	ResolvedRegion string
}
