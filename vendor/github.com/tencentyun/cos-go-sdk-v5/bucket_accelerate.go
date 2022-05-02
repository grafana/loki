package cos

import (
	"context"
	"encoding/xml"
	"net/http"
)

type BucketPutAccelerateOptions struct {
	XMLName xml.Name `xml:"AccelerateConfiguration"`
	Status  string   `xml:"Status,omitempty"`
	Type    string   `xml:"Type,omitempty"`
}
type BucketGetAccelerateResult BucketPutAccelerateOptions

func (s *BucketService) PutAccelerate(ctx context.Context, opt *BucketPutAccelerateOptions) (*Response, error) {
	sendOpt := &sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     "/?accelerate",
		method:  http.MethodPut,
		body:    opt,
	}
	resp, err := s.client.doRetry(ctx, sendOpt)
	return resp, err
}

func (s *BucketService) GetAccelerate(ctx context.Context) (*BucketGetAccelerateResult, *Response, error) {
	var res BucketGetAccelerateResult
	sendOpt := &sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     "/?accelerate",
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.doRetry(ctx, sendOpt)
	return &res, resp, err
}
