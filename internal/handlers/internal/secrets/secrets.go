package secrets

import (
	"github.com/ViaQ/logerr/kverrors"
	"github.com/ViaQ/loki-operator/internal/manifests"

	corev1 "k8s.io/api/core/v1"
)

// Extract reads a k8s secret into a manifest object storage struct if valid.
func Extract(s *corev1.Secret) (*manifests.ObjectStorage, error) {
	// Extract and validate mandatory fields
	endpoint, ok := s.Data["endpoint"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "endpoint")
	}
	buckets, ok := s.Data["bucketnames"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "bucketnames")
	}
	// TODO buckets are comma-separated list
	id, ok := s.Data["access_key_id"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "access_key_id")
	}
	secret, ok := s.Data["access_key_secret"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "access_key_secret")
	}

	// Extract and validate optional fields
	region, ok := s.Data["region"]
	if !ok {
		region = []byte("")
	}

	return &manifests.ObjectStorage{
		Endpoint:        string(endpoint),
		Buckets:         string(buckets),
		AccessKeyID:     string(id),
		AccessKeySecret: string(secret),
		Region:          string(region),
	}, nil
}
