package api

import (
	"github.com/baidubce/bce-sdk-go/bce"
)

type BosRequest struct {
	bce.BceRequest
	BucketName      string
	ObjectKey       string
	Tracker         []RequestTracker
	IsObjectRequest bool
}

func (r *BosRequest) SetBucket(bkt string) {
	r.BucketName = bkt
}

func (r *BosRequest) SetObject(obj string) {
	r.ObjectKey = obj
}

func (r *BosRequest) SetIsObjectReq(flag bool) {
	r.IsObjectRequest = flag
}

func (r *BosRequest) Bucket() string {
	return r.BucketName
}

func (r *BosRequest) Object() string {
	return r.ObjectKey
}

func (r *BosRequest) IsObjectReq() bool {
	return r.IsObjectRequest
}

type BosResponse struct {
	bce.BceResponse
	Handler []ResponseHandler
}
