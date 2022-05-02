package cos

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
)

// Notice bucket_inventory only for test. can not use

// BucketGetInventoryResult same struct to options
type BucketGetInventoryResult BucketPutInventoryOptions

// BucketListInventoryConfiguartion same struct to options
type BucketListInventoryConfiguartion BucketPutInventoryOptions

// BucketInventoryFilter ...
type BucketInventoryFilter struct {
	Prefix string `xml:"Prefix,omitempty"`
}

// BucketInventoryOptionalFields ...
type BucketInventoryOptionalFields struct {
	BucketInventoryFields []string `xml:"Field,omitempty"`
}

// BucketInventorySchedule ...
type BucketInventorySchedule struct {
	Frequency string `xml:"Frequency"`
}

// BucketInventoryEncryption ...
type BucketInventoryEncryption struct {
	SSECOS string `xml:"SSE-COS"`
}

// BucketInventoryDestination ...
type BucketInventoryDestination struct {
	Bucket     string                     `xml:"Bucket"`
	AccountId  string                     `xml:"AccountId,omitempty"`
	Prefix     string                     `xml:"Prefix,omitempty"`
	Format     string                     `xml:"Format"`
	Encryption *BucketInventoryEncryption `xml:"Encryption,omitempty"`
}

// BucketPutInventoryOptions ...
type BucketPutInventoryOptions struct {
	XMLName                xml.Name                       `xml:"InventoryConfiguration"`
	ID                     string                         `xml:"Id"`
	IsEnabled              string                         `xml:"IsEnabled"`
	IncludedObjectVersions string                         `xml:"IncludedObjectVersions"`
	Filter                 *BucketInventoryFilter         `xml:"Filter,omitempty"`
	OptionalFields         *BucketInventoryOptionalFields `xml:"OptionalFields,omitempty"`
	Schedule               *BucketInventorySchedule       `xml:"Schedule"`
	Destination            *BucketInventoryDestination    `xml:"Destination>COSBucketDestination"`
}

// ListBucketInventoryConfigResult result of ListBucketInventoryConfiguration
type ListBucketInventoryConfigResult struct {
	XMLName                 xml.Name                           `xml:"ListInventoryConfigurationResult"`
	InventoryConfigurations []BucketListInventoryConfiguartion `xml:"InventoryConfiguration,omitempty"`
	IsTruncated             bool                               `xml:"IsTruncated,omitempty"`
	ContinuationToken       string                             `xml:"ContinuationToken,omitempty"`
	NextContinuationToken   string                             `xml:"NextContinuationToken,omitempty"`
}

// PutBucketInventory https://cloud.tencent.com/document/product/436/33707
func (s *BucketService) PutInventory(ctx context.Context, id string, opt *BucketPutInventoryOptions) (*Response, error) {
	u := fmt.Sprintf("/?inventory&id=%s", id)
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     u,
		method:  http.MethodPut,
		body:    opt,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	return resp, err

}

// GetBucketInventory https://cloud.tencent.com/document/product/436/33705
func (s *BucketService) GetInventory(ctx context.Context, id string) (*BucketGetInventoryResult, *Response, error) {
	u := fmt.Sprintf("/?inventory&id=%s", id)
	var res BucketGetInventoryResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     u,
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	return &res, resp, err
}

// DeleteBucketInventory https://cloud.tencent.com/document/product/436/33704
func (s *BucketService) DeleteInventory(ctx context.Context, id string) (*Response, error) {
	u := fmt.Sprintf("/?inventory&id=%s", id)
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     u,
		method:  http.MethodDelete,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	return resp, err
}

// ListBucketInventoryConfigurations https://cloud.tencent.com/document/product/436/33706
func (s *BucketService) ListInventoryConfigurations(ctx context.Context, token string) (*ListBucketInventoryConfigResult, *Response, error) {
	var res ListBucketInventoryConfigResult
	var u string
	if token == "" {
		u = "/?inventory"
	} else {
		u = fmt.Sprintf("/?inventory&continuation-token=%s", encodeURIComponent(token))
	}
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     u,
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	return &res, resp, err

}
