package rules

import (
	"github.com/ViaQ/logerr/v2/kverrors"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests"

	corev1 "k8s.io/api/core/v1"
)

// ExtractRulerSecret reads a k8s secret infto a ruler secret struct if valid.
func ExtractRulerSecret(s *corev1.Secret, t lokiv1beta1.RemoteWriteAuthType) (*manifests.RulerSecret, error) {
	switch t {
	case lokiv1beta1.BasicAuthorization:
		username, ok := s.Data["username"]
		if !ok {
			return nil, kverrors.New("missing basic auth username", "field", "username")
		}

		password, ok := s.Data["password"]
		if !ok {
			return nil, kverrors.New("missing basic auth password", "field", "username")
		}

		return &manifests.RulerSecret{Username: string(username), Password: string(password)}, nil
	case lokiv1beta1.HeaderAuthorization:
		token, ok := s.Data["bearer_token"]
		if !ok {
			return nil, kverrors.New("missing bearer token", "field", "bearer_token")
		}

		return &manifests.RulerSecret{BearerToken: string(token)}, nil
	default:
		return nil, kverrors.New("unknown ruler secret type", "type", t)
	}
}
