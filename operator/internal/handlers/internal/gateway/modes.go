package gateway

import (
	"github.com/ViaQ/logerr/v2/kverrors"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

func validateModes(stack *lokiv1.LokiStack) error {
	switch stack.Spec.Tenants.Mode {
	case lokiv1.Static:
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
	case lokiv1.Dynamic:
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
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork, lokiv1.Passthrough:
		if stack.Spec.Tenants.Authentication != nil {
			return kverrors.New("incompatible configuration - custom tenants configuration not required")
		}

		if stack.Spec.Tenants.Authorization != nil {
			return kverrors.New("incompatible configuration - custom tenants configuration not required")
		}
	}

	return nil
}
