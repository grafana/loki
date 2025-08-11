// service.go - the service APIs definition supported by the BOS service

// Package api defines all APIs supported by the BOS service of BCE.
package api

import (
	"encoding/json"
	"fmt"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/http"
)

// PutUserQuota - put quota configuration for the caller(must be master user)
//
// PARAMS:
//   - cli: the client agent which can perform sending request
//
// RETURNS:
//   - error: nil if ok otherwise the specific error
func PutUserQuota(cli bce.Client, args *UserQuotaArgs, ctx *BosContext, options ...Option) error {
	req := &BosRequest{}
	req.SetMethod(http.PUT)
	req.SetParam("userQuota", "")
	resp := &BosResponse{}
	// handle options to set the header/params of request
	if err := handleOptions(req, options); err != nil {
		return bce.NewBceClientError(fmt.Sprintf("Handle options occur error: %s", err))
	}
	bodyBytes, _ := json.Marshal(args)
	body, err := bce.NewBodyFromBytes(bodyBytes)
	if err != nil {
		return err
	}
	req.SetBody(body)
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	return nil
}

// GetUserQuota - get the quota of the caller(must be master user)
//
// PARAMS:
//   - cli: the client agent which can perform sending request
//   - ctx: the context to control the request
//   - options: the function set to set HTTP headers/params
//
// RETURNS:
//   - result: the user quota info
//   - error: nil if success otherwise the specific error
func GetUserQuota(cli bce.Client, ctx *BosContext, options ...Option) (*UserQuotaArgs, error) {
	req := &BosRequest{}
	req.SetMethod(http.GET)
	req.SetParam("userQuota", "")
	// handle options to set the header/params of request
	if err := handleOptions(req, options); err != nil {
		return nil, bce.NewBceClientError(fmt.Sprintf("Handle options occur error: %s", err))
	}
	resp := &BosResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return nil, err
	}
	result := &UserQuotaArgs{}
	if err := resp.ParseJsonBody(result); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteUserQuota - delete the quota for the caller(must be master user)
//
// PARAMS:
//   - cli: the client agent which can perform sending request
//   - bucket: the bucket name
//   - ctx: the context to control the request
//   - options: the function set to set HTTP headers/params
//
// RETURNS:
//   - error: nil if success otherwise the specific error
func DeleteUserQuota(cli bce.Client, ctx *BosContext, options ...Option) error {
	req := &BosRequest{}
	req.SetMethod(http.DELETE)
	req.SetParam("userQuota", "")
	// handle options to set the header/params of request
	if err := handleOptions(req, options); err != nil {
		return bce.NewBceClientError(fmt.Sprintf("Handle options occur error: %s", err))
	}
	resp := &BosResponse{}
	if err := SendRequest(cli, req, resp, ctx); err != nil {
		return err
	}
	if resp.IsFail() {
		return resp.ServiceError()
	}
	defer func() { resp.Body().Close() }()
	return nil
}
