package certrotation

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/user"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
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
	rotation, err := ParseRotation(cfg)
	if err != nil {
		return err
	}
	opts.Rotation = rotation

	clock := time.Now
	opts.Signer.Rotation = signerRotation{
		Clock: clock,
	}

	if opts.Certificates == nil {
		opts.Certificates = make(map[string]SelfSignedCertKey)
	}
	for _, name := range ComponentCertSecretNames(opts.StackName) {
		r := certificateRotation{
			Clock:    clock,
			UserInfo: defaultUserInfo,
			Hostnames: sets.New[string](
				fmt.Sprintf("%s.%s.svc", name, opts.StackNamespace),
				fmt.Sprintf("%s.%s.svc.cluster.local", name, opts.StackNamespace),
			),
		}

		cert, ok := opts.Certificates[name]
		if !ok {
			cert = SelfSignedCertKey{}
		}
		cert.Rotation = r
		opts.Certificates[name] = cert
	}

	return nil
}
