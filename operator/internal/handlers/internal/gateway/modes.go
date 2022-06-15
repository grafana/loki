package gateway

import (
	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
)

// ValidateModes validates the tenants mode specification.
func ValidateModes(stack lokiv1beta1.LokiStack) error {
	if stack.Spec.Tenants.Mode == lokiv1beta1.Static {
		if stack.Spec.Tenants.Authentication == nil {
			return kverrors.New("mandatory configuration - missing tenants' authentication configuration")
		}

		if stack.Spec.Tenants.Authorization == nil || stack.Spec.Tenants.Authorization.Roles == nil {
			return kverrors.New("mandatory configuration - missing roles configuration")
		}

		if stack.Spec.Tenants.Authorization == nil || stack.Spec.Tenants.Authorization.RoleBindings == nil {
			return kverrors.New("mandatory configuration - missing role bindings configuration")
		}

		if stack.Spec.Tenants.Authorization != nil && stack.Spec.Tenants.Authorization.OPA != nil {
			return kverrors.New("incompatible configuration - OPA URL not required for mode static")
		}
	}

	if stack.Spec.Tenants.Mode == lokiv1beta1.Dynamic {
		if stack.Spec.Tenants.Authentication == nil {
			return kverrors.New("mandatory configuration - missing tenants configuration")
		}

		if stack.Spec.Tenants.Authorization == nil || stack.Spec.Tenants.Authorization.OPA == nil {
			return kverrors.New("mandatory configuration - missing OPA Url")
		}

		if stack.Spec.Tenants.Authorization != nil && stack.Spec.Tenants.Authorization.Roles != nil {
			return kverrors.New("incompatible configuration - static roles not required for mode dynamic")
		}

		if stack.Spec.Tenants.Authorization != nil && stack.Spec.Tenants.Authorization.RoleBindings != nil {
			return kverrors.New("incompatible configuration - static roleBindings not required for mode dynamic")
		}
	}

	if stack.Spec.Tenants.Mode == lokiv1beta1.OpenshiftLogging {
		if stack.Spec.Tenants.Authentication != nil {
			return kverrors.New("incompatible configuration - custom tenants configuration not required")
		}

		if stack.Spec.Tenants.Authorization != nil {
			return kverrors.New("incompatible configuration - custom tenants configuration not required")
		}
	}

	return nil
}
