package handlers

import (
	"context"
	"fmt"
	"os"

	"github.com/grafana/loki-operator/internal/manifests/openshift"

	"github.com/ViaQ/logerr/kverrors"
	"github.com/ViaQ/logerr/log"
	lokiv1beta1 "github.com/grafana/loki-operator/api/v1beta1"
	"github.com/grafana/loki-operator/internal/external/k8s"
	"github.com/grafana/loki-operator/internal/handlers/internal/gateway"
	"github.com/grafana/loki-operator/internal/handlers/internal/secrets"
	"github.com/grafana/loki-operator/internal/manifests"
	"github.com/grafana/loki-operator/internal/metrics"
	"github.com/grafana/loki-operator/internal/status"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateOrUpdateLokiStack handles LokiStack create and update events.
func CreateOrUpdateLokiStack(ctx context.Context, req ctrl.Request, k k8s.Client, s *runtime.Scheme, flags manifests.FeatureFlags) error {
	ll := log.WithValues("lokistack", req.NamespacedName, "event", "createOrUpdate")

	var stack lokiv1beta1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			// maybe the user deleted it before we could react? Either way this isn't an issue
			ll.Error(err, "could not find the requested loki stack", "name", req.NamespacedName)
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	img := os.Getenv(manifests.EnvRelatedImageLoki)
	if img == "" {
		img = manifests.DefaultContainerImage
	}

	gwImg := os.Getenv(manifests.EnvRelatedImageGateway)
	if gwImg == "" {
		gwImg = manifests.DefaultLokiStackGatewayImage
	}

	var s3secret corev1.Secret
	key := client.ObjectKey{Name: stack.Spec.Storage.Secret.Name, Namespace: stack.Namespace}
	if err := k.Get(ctx, key, &s3secret); err != nil {
		if apierrors.IsNotFound(err) {
			return status.SetDegradedCondition(ctx, k, req,
				"Missing object storage secret",
				lokiv1beta1.ReasonMissingObjectStorageSecret,
			)
		}
		return kverrors.Wrap(err, "failed to lookup lokistack s3 secret", "name", key)
	}

	storage, err := secrets.Extract(&s3secret)
	if err != nil {
		return status.SetDegradedCondition(ctx, k, req,
			"Invalid object storage secret contents",
			lokiv1beta1.ReasonInvalidObjectStorageSecret,
		)
	}

	var (
		baseDomain      string
		tenantSecrets   []*manifests.TenantSecrets
		tenantConfigMap map[string]openshift.TenantData
	)
	if flags.EnableGateway && stack.Spec.Tenants != nil {
		if err = gateway.ValidateModes(stack); err != nil {
			return status.SetDegradedCondition(ctx, k, req,
				fmt.Sprintf("Invalid tenants configuration: %s", err),
				lokiv1beta1.ReasonInvalidTenantsConfiguration,
			)
		}

		if stack.Spec.Tenants.Mode != lokiv1beta1.OpenshiftLogging {
			tenantSecrets, err = gateway.GetTenantSecrets(ctx, k, req, &stack)
			if err != nil {
				return err
			}
		}

		if stack.Spec.Tenants.Mode == lokiv1beta1.OpenshiftLogging {
			baseDomain, err = gateway.GetOpenShiftBaseDomain(ctx, k, req)
			if err != nil {
				return nil
			}

			// extract the existing tenant's id, cookieSecret if exists, otherwise create new.
			tenantConfigMap = gateway.GetTenantConfigMapData(ctx, k, req)
		}
	}

	// Here we will translate the lokiv1beta1.LokiStack options into manifest options
	opts := manifests.Options{
		Name:              req.Name,
		Namespace:         req.Namespace,
		Image:             img,
		GatewayImage:      gwImg,
		GatewayBaseDomain: baseDomain,
		Stack:             stack.Spec,
		Flags:             flags,
		ObjectStorage:     *storage,
		TenantSecrets:     tenantSecrets,
		TenantConfigMap:   tenantConfigMap,
	}

	ll.Info("begin building manifests")

	if optErr := manifests.ApplyDefaultSettings(&opts); optErr != nil {
		ll.Error(optErr, "failed to conform options to build settings")
		return optErr
	}

	if flags.EnableGateway {
		if optErr := manifests.ApplyGatewayDefaultOptions(&opts); optErr != nil {
			ll.Error(optErr, "failed to apply defaults options to gateway settings ")
			return err
		}
	}

	objects, err := manifests.BuildAll(opts)
	if err != nil {
		ll.Error(err, "failed to build manifests")
		return err
	}
	ll.Info("manifests built", "count", len(objects))

	var errCount int32

	for _, obj := range objects {
		l := ll.WithValues(
			"object_name", obj.GetName(),
			"object_kind", obj.GetObjectKind(),
		)

		if isNamespaceScoped(obj) {
			obj.SetNamespace(req.Namespace)

			if err := ctrl.SetControllerReference(&stack, obj, s); err != nil {
				l.Error(err, "failed to set controller owner reference to resource")
				errCount++
				continue
			}
		}

		desired := obj.DeepCopyObject().(client.Object)
		mutateFn := manifests.MutateFuncFor(obj, desired)

		op, err := ctrl.CreateOrUpdate(ctx, k, obj, mutateFn)
		if err != nil {
			l.Error(err, "failed to configure resource")
			errCount++
			continue
		}

		l.Info(fmt.Sprintf("Resource has been %s", op))
	}

	if errCount > 0 {
		return kverrors.New("failed to configure lokistack resources", "name", req.NamespacedName)
	}

	// 1x.extra-small is used only for development, so the metrics will not
	// be collected.
	if opts.Stack.Size != lokiv1beta1.SizeOneXExtraSmall {
		metrics.Collect(&opts.Stack, opts.Name)
	}

	return nil
}

func isNamespaceScoped(obj client.Object) bool {
	switch obj.(type) {
	case *rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding:
		return false
	default:
		return true
	}
}
