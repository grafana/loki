package handlers

import (
	"context"
	"fmt"
	"os"
	"time"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/handlers/internal/gateway"
	"github.com/grafana/loki/operator/internal/handlers/internal/openshift"
	"github.com/grafana/loki/operator/internal/handlers/internal/rules"
	"github.com/grafana/loki/operator/internal/handlers/internal/serviceaccounts"
	"github.com/grafana/loki/operator/internal/handlers/internal/storage"
	"github.com/grafana/loki/operator/internal/handlers/internal/tlsprofile"
	"github.com/grafana/loki/operator/internal/manifests"
	manifests_openshift "github.com/grafana/loki/operator/internal/manifests/openshift"
	storageoptions "github.com/grafana/loki/operator/internal/manifests/storage"
	"github.com/grafana/loki/operator/internal/metrics"
	"github.com/grafana/loki/operator/internal/status"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	defaultCAKey = "service-ca.crt"
)

// CreateOrUpdateLokiStack handles LokiStack create and update events.
func CreateOrUpdateLokiStack(
	ctx context.Context,
	log logr.Logger,
	req ctrl.Request,
	k k8s.Client,
	s *runtime.Scheme,
	fg configv1.FeatureGates,
) error {
	ll := log.WithValues("lokistack", req.NamespacedName, "event", "createOrUpdate")

	var stack lokiv1.LokiStack
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

	var storageSecret corev1.Secret
	key := client.ObjectKey{Name: stack.Spec.Storage.Secret.Name, Namespace: stack.Namespace}
	if err := k.Get(ctx, key, &storageSecret); err != nil {
		if apierrors.IsNotFound(err) {
			return &status.DegradedError{
				Message: "Missing object storage secret",
				Reason:  lokiv1.ReasonMissingObjectStorageSecret,
				Requeue: false,
			}
		}
		return kverrors.Wrap(err, "failed to lookup lokistack storage secret", "name", key)
	}

	objStore, err := storage.ExtractSecret(&storageSecret, stack.Spec.Storage.Secret.Type)
	if err != nil {
		return &status.DegradedError{
			Message: fmt.Sprintf("Invalid object storage secret contents: %s", err),
			Reason:  lokiv1.ReasonInvalidObjectStorageSecret,
			Requeue: false,
		}
	}

	storageSchemas, err := storageoptions.BuildSchemaConfig(
		time.Now().UTC(),
		stack.Spec.Storage,
		stack.Status.Storage,
	)
	if err != nil {
		return &status.DegradedError{
			Message: fmt.Sprintf("Invalid object storage schema contents: %s", err),
			Reason:  lokiv1.ReasonInvalidObjectStorageSchema,
			Requeue: false,
		}
	}

	objStore.Schemas = storageSchemas

	if stack.Spec.Storage.TLS != nil {
		tlsConfig := stack.Spec.Storage.TLS

		if tlsConfig.CA == "" {
			return &status.DegradedError{
				Message: "Missing object storage CA config map",
				Reason:  lokiv1.ReasonMissingObjectStorageCAConfigMap,
				Requeue: false,
			}
		}

		var cm corev1.ConfigMap
		key := client.ObjectKey{Name: tlsConfig.CA, Namespace: stack.Namespace}
		if err = k.Get(ctx, key, &cm); err != nil {
			if apierrors.IsNotFound(err) {
				return &status.DegradedError{
					Message: "Missing object storage CA config map",
					Reason:  lokiv1.ReasonMissingObjectStorageCAConfigMap,
					Requeue: false,
				}
			}
			return kverrors.Wrap(err, "failed to lookup lokistack object storage CA config map", "name", key)
		}

		caKey := defaultCAKey
		if tlsConfig.CAKey != "" {
			caKey = tlsConfig.CAKey
		}

		if !storage.IsValidCAConfigMap(&cm, caKey) {
			return &status.DegradedError{
				Message: "Invalid object storage CA configmap contents: missing key or no contents",
				Reason:  lokiv1.ReasonInvalidObjectStorageCAConfigMap,
				Requeue: false,
			}
		}

		objStore.TLS = &storageoptions.TLSConfig{CA: cm.Name, Key: caKey}
	}

	var (
		baseDomain    string
		tenantSecrets []*manifests.TenantSecrets
		tenantConfigs map[string]manifests.TenantConfig
	)
	if fg.LokiStackGateway && stack.Spec.Tenants == nil {
		return &status.DegradedError{
			Message: "Invalid tenants configuration - TenantsSpec cannot be nil when gateway flag is enabled",
			Reason:  lokiv1.ReasonInvalidTenantsConfiguration,
			Requeue: false,
		}
	} else if fg.LokiStackGateway && stack.Spec.Tenants != nil {
		if err = gateway.ValidateModes(stack); err != nil {
			return &status.DegradedError{
				Message: fmt.Sprintf("Invalid tenants configuration: %s", err),
				Reason:  lokiv1.ReasonInvalidTenantsConfiguration,
				Requeue: false,
			}
		}

		switch stack.Spec.Tenants.Mode {
		case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
			baseDomain, err = gateway.GetOpenShiftBaseDomain(ctx, k, req)
			if err != nil {
				return err
			}

			if stack.Spec.Proxy == nil {
				// If the LokiStack has no proxy set but there is a cluster-wide proxy setting,
				// set the LokiStack proxy to that.
				ocpProxy, proxyErr := openshift.GetProxy(ctx, k)
				if proxyErr != nil {
					return proxyErr
				}

				stack.Spec.Proxy = ocpProxy
			}
		default:
			tenantSecrets, err = gateway.GetTenantSecrets(ctx, k, req, &stack)
			if err != nil {
				return err
			}
		}

		// extract the existing tenant's id, cookieSecret if exists, otherwise create new.
		tenantConfigs, err = gateway.GetTenantConfigSecretData(ctx, k, req)
		if err != nil {
			ll.Error(err, "error in getting tenant secret data")
		}
	}

	var (
		alertingRules  []lokiv1.AlertingRule
		recordingRules []lokiv1.RecordingRule
		rulerConfig    *lokiv1.RulerConfigSpec
		rulerSecret    *manifests.RulerSecret
		ocpAmEnabled   bool
		ocpUWAmEnabled bool
	)
	if stack.Spec.Rules != nil && stack.Spec.Rules.Enabled {
		alertingRules, recordingRules, err = rules.List(ctx, k, req.Namespace, stack.Spec.Rules)
		if err != nil {
			ll.Error(err, "failed to lookup rules", "spec", stack.Spec.Rules)
		}

		rulerConfig, err = rules.GetRulerConfig(ctx, k, req)
		if err != nil {
			ll.Error(err, "failed to lookup ruler config", "key", req.NamespacedName)
		}

		if rulerConfig != nil && rulerConfig.RemoteWriteSpec != nil && rulerConfig.RemoteWriteSpec.ClientSpec != nil {
			var rs corev1.Secret
			key := client.ObjectKey{Name: rulerConfig.RemoteWriteSpec.ClientSpec.AuthorizationSecretName, Namespace: stack.Namespace}
			if err = k.Get(ctx, key, &rs); err != nil {
				if apierrors.IsNotFound(err) {
					return &status.DegradedError{
						Message: "Missing ruler remote write authorization secret",
						Reason:  lokiv1.ReasonMissingRulerSecret,
						Requeue: false,
					}
				}
				return kverrors.Wrap(err, "failed to lookup lokistack ruler secret", "name", key)
			}

			rulerSecret, err = rules.ExtractRulerSecret(&rs, rulerConfig.RemoteWriteSpec.ClientSpec.AuthorizationType)
			if err != nil {
				return &status.DegradedError{
					Message: "Invalid ruler remote write authorization secret contents",
					Reason:  lokiv1.ReasonInvalidRulerSecret,
					Requeue: false,
				}
			}
		}

		ocpAmEnabled, err = openshift.AlertManagerSVCExists(ctx, stack.Spec, k)
		if err != nil {
			ll.Error(err, "failed to check OCP AlertManager")
			return err
		}

		ocpUWAmEnabled, err = openshift.UserWorkloadAlertManagerSVCExists(ctx, stack.Spec, k)
		if err != nil {
			ll.Error(err, "failed to check OCP User Workload AlertManager")
			return err
		}
	} else {
		// Clean up ruler resources
		err = rules.RemoveRulesConfigMap(ctx, req, k)
		if err != nil {
			ll.Error(err, "failed to remove rules configmap")
			return err
		}

		err = rules.RemoveRuler(ctx, req, k)
		if err != nil {
			ll.Error(err, "failed to remove ruler statefulset")
			return err
		}
	}

	certRotationRequiredAt := ""
	if stack.Annotations != nil {
		certRotationRequiredAt = stack.Annotations[manifests.AnnotationCertRotationRequiredAt]
	}

	// Here we will translate the lokiv1.LokiStack options into manifest options
	opts := manifests.Options{
		Name:                   req.Name,
		Namespace:              req.Namespace,
		Image:                  img,
		GatewayImage:           gwImg,
		GatewayBaseDomain:      baseDomain,
		Stack:                  stack.Spec,
		Gates:                  fg,
		ObjectStorage:          *objStore,
		CertRotationRequiredAt: certRotationRequiredAt,
		AlertingRules:          alertingRules,
		RecordingRules:         recordingRules,
		Ruler: manifests.Ruler{
			Spec:   rulerConfig,
			Secret: rulerSecret,
		},
		Tenants: manifests.Tenants{
			Secrets: tenantSecrets,
			Configs: tenantConfigs,
		},
		OpenShiftOptions: manifests_openshift.Options{
			BuildOpts: manifests_openshift.BuildOptions{
				AlertManagerEnabled:             ocpAmEnabled,
				UserWorkloadAlertManagerEnabled: ocpUWAmEnabled,
			},
		},
	}

	ll.Info("begin building manifests")

	if optErr := manifests.ApplyDefaultSettings(&opts); optErr != nil {
		ll.Error(optErr, "failed to conform options to build settings")
		return optErr
	}

	if fg.LokiStackGateway {
		if optErr := manifests.ApplyGatewayDefaultOptions(&opts); optErr != nil {
			ll.Error(optErr, "failed to apply defaults options to gateway settings")
			return optErr
		}
	}

	tlsProfileType := configv1.TLSProfileType(fg.TLSProfile)
	// Overwrite the profile from the flags and use the profile from the apiserver instead
	if fg.OpenShift.ClusterTLSPolicy {
		tlsProfileType = configv1.TLSProfileType("")
	}

	tlsProfile, err := tlsprofile.GetTLSSecurityProfile(ctx, k, tlsProfileType)
	if err != nil {
		// The API server is not guaranteed to be there nor have a result.
		ll.Error(err, "failed to get security profile. will use default tls profile.")
	}

	if optErr := manifests.ApplyTLSSettings(&opts, tlsProfile); optErr != nil {
		ll.Error(optErr, "failed to conform options to tls profile settings")
		return optErr
	}

	objects, err := manifests.BuildAll(opts)
	if err != nil {
		ll.Error(err, "failed to build manifests")
		return err
	}

	ll.Info("manifests built", "count", len(objects))

	// The status is updated before the objects are actually created to
	// avoid the scenario in which the configmap is successfully created or
	// updated and another resource is not. This would cause the status to
	// be possibly misaligned with the configmap, which could lead to
	// a user possibly being unable to read logs.
	if err := status.SetStorageSchemaStatus(ctx, k, req, storageSchemas); err != nil {
		ll.Error(err, "failed to set storage schema status")
		return err
	}

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

		depAnnotations, err := dependentAnnotations(ctx, k, obj)
		if err != nil {
			return err
		}

		desired := obj.DeepCopyObject().(client.Object)
		mutateFn := manifests.MutateFuncFor(obj, desired, depAnnotations)

		op, err := ctrl.CreateOrUpdate(ctx, k, obj, mutateFn)
		if err != nil {
			l.Error(err, "failed to configure resource")
			errCount++
			continue
		}

		msg := fmt.Sprintf("Resource has been %s", op)
		switch op {
		case ctrlutil.OperationResultNone:
			l.V(1).Info(msg)
		default:
			l.Info(msg)
		}
	}

	if errCount > 0 {
		return kverrors.New("failed to configure lokistack resources", "name", req.NamespacedName)
	}

	// 1x.extra-small is used only for development, so the metrics will not
	// be collected.
	if opts.Stack.Size != lokiv1.SizeOneXExtraSmall {
		metrics.Collect(&opts.Stack, opts.Name)
	}

	return nil
}

func dependentAnnotations(ctx context.Context, k k8s.Client, obj client.Object) (map[string]string, error) {
	a := obj.GetAnnotations()
	saName, ok := a[corev1.ServiceAccountNameKey]
	if !ok || saName == "" {
		return nil, nil
	}

	key := client.ObjectKey{Name: saName, Namespace: obj.GetNamespace()}
	uid, err := serviceaccounts.GetUID(ctx, k, key)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		corev1.ServiceAccountUIDKey: uid,
	}, nil
}

func isNamespaceScoped(obj client.Object) bool {
	switch obj.(type) {
	case *rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding:
		return false
	default:
		return true
	}
}
