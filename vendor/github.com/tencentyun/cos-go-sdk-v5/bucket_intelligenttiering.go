package cos

import (
	"context"
	"encoding/xml"
	"net/http"
)

type BucketIntelligentTieringTransition struct {
	Days            int `xml:"Days,omitempty"`
	RequestFrequent int `xml:"RequestFrequent,omitempty"`
}

type BucketPutIntelligentTieringOptions struct {
	XMLName    xml.Name                            `xml:"IntelligentTieringConfiguration"`
	Status     string                              `xml:"Status,omitempty"`
	Transition *BucketIntelligentTieringTransition `xml:"Transition,omitempty"`
}

type BucketGetIntelligentTieringResult BucketPutIntelligentTieringOptions

func (s *BucketService) PutIntelligentTiering(ctx context.Context, opt *BucketPutIntelligentTieringOptions) (*Response, error) {
	if opt != nil && opt.Transition != nil {
		opt.Transition.RequestFrequent = 1
	}
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     "/?intelligenttiering",
		method:  http.MethodPut,
		body:    opt,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	return resp, err
}

func (s *BucketService) GetIntelligentTiering(ctx context.Context) (*BucketGetIntelligentTieringResult, *Response, error) {
	var res BucketGetIntelligentTieringResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     "/?intelligenttiering",
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	return &res, resp, err

}
