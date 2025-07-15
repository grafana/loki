package rules

import (
	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests"
)

// ExtractRulerSecret reads a k8s secret infto a ruler secret struct if valid.
func ExtractRulerSecret(s *corev1.Secret, t lokiv1.RemoteWriteAuthType) (*manifests.RulerSecret, error) {
	switch t {
	case lokiv1.BasicAuthorization:
		username := s.Data["username"]
		if len(username) == 0 {
			return nil, kverrors.New("missing basic auth username", "field", "username")
		}

		password := s.Data["password"]
		if len(password) == 0 {
			return nil, kverrors.New("missing basic auth password", "field", "password")
		}

		return &manifests.RulerSecret{Username: string(username), Password: string(password)}, nil
	case lokiv1.BearerAuthorization:
		token := s.Data["bearer_token"]
		if len(token) == 0 {
			return nil, kverrors.New("missing bearer token", "field", "bearer_token")
		}

		return &manifests.RulerSecret{BearerToken: string(token)}, nil
	default:
		return nil, kverrors.New("unknown ruler secret type", "type", t)
	}
}
