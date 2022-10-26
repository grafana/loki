package certrotation

import (
	"fmt"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	"github.com/imdario/mergo"
	"k8s.io/apiserver/pkg/authentication/user"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var defaultUserInfo = &user.DefaultInfo{Name: "system:lokistacks", Groups: []string{"system:logging"}}

// BuildAll builds all secrets and configmaps containing
// CA certificates, CA bundles and client certificates for
// a LokiStack.
func BuildAll(opts Options) ([]client.Object, error) {
	res := make([]client.Object, 0)

	obj, err := buildSigningCASecret(&opts)
	if err != nil {
		return nil, err
	}
	res = append(res, obj)

	obj, err = buildCABundle(&opts)
	if err != nil {
		return nil, err
	}
	res = append(res, obj)

	objs, err := buildTargetCertKeyPairSecrets(opts)
	if err != nil {
		return nil, err
	}
	res = append(res, objs...)

	return res, nil
}

// ApplyDefaultSettings merges the default options with the ones we give.
func ApplyDefaultSettings(opts *Options, cfg configv1.BuiltInCertManagement) error {
	caValidity, err := time.ParseDuration(cfg.CACertValidity)
	if err != nil {
		return kverrors.Wrap(err, "failed to parse CA validity duration", "value", cfg.CACertValidity)
	}

	caRefresh, err := time.ParseDuration(cfg.CACertRefresh)
	if err != nil {
		return kverrors.Wrap(err, "failed to parse CA refresh duration", "value", cfg.CACertRefresh)
	}

	certValidity, err := time.ParseDuration(cfg.CertValidity)
	if err != nil {
		return kverrors.Wrap(err, "failed to parse target certificate validity duration", "value", cfg.CertValidity)
	}

	certRefresh, err := time.ParseDuration(cfg.CertRefresh)
	if err != nil {
		return kverrors.Wrap(err, "failed to parse target certificate refresh duration", "value", cfg.CertRefresh)
	}

	if opts.Certificates == nil {
		opts.Certificates = make(map[string]SelfSignedCertKey)
	}

	for _, name := range ComponentCertSecretNames(opts.StackName) {
		c := newCreator(name, opts.StackNamespace)

		cert, ok := opts.Certificates[name]
		if !ok {
			cert = SelfSignedCertKey{creator: c}
		} else {
			cert.creator = c
		}

		opts.Certificates[name] = cert
	}

	certOpts := Options{
		CACertValidity:     caValidity,
		CACertRefresh:      caRefresh,
		TargetCertValidity: certValidity,
		TargetCertRefresh:  certRefresh,
	}

	if err := mergo.Merge(opts, certOpts, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge cert rotation options")
	}

	return nil
}

func newCreator(name, namespace string) certificateRotation {
	return certificateRotation{
		UserInfo: defaultUserInfo,
		Hostnames: []string{
			fmt.Sprintf("%s.%s.svc", name, namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace),
		},
	}
}
