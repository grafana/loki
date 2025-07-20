package api

import (
	"github.com/baidubce/bce-sdk-go/bce"
)

type BosRequest struct {
	bce.BceRequest
	BucketName string
	ObjectKey  string
}

func (r *BosRequest) SetBucket(bkt string) {
	r.BucketName = bkt
}

func (r *BosRequest) SetObject(obj string) {
	r.ObjectKey = obj
}

func (r *BosRequest) Bucket() string {
	return r.BucketName
}

func (r *BosRequest) Object() string {
	return r.ObjectKey
}

type BosResponse struct {
	bce.BceResponse
}
