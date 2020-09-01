package multitenancy

import (
	"context"
	"testing"

	"github.com/weaveworks/common/user"
)

func Test_Validate(t *testing.T) {
	// multi-tenancy enabled false
	cfg := Config{
		Enabled: false,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate cfg with enabled: false failed: " + err.Error())
	}

	// multi-tenancy enabled, type auth
	cfg = Config{
		Enabled: true,
		Type:    "auth",
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate cfg with enabled: true, type auth failed: " + err.Error())
	}

	// multi-tenancy enabled, type label
	cfg = Config{
		Enabled:   true,
		Type:      "label",
		Label:     "test",
		Undefined: "test-2",
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate cfg with enabled: true, type label failed: " + err.Error())
	}

	// multi-tenancy enabled, type auth, label and undefined set
	cfg = Config{
		Enabled:   true,
		Type:      "auth",
		Label:     "test",
		Undefined: "test2",
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate cfg with enabled: true, type auth, label and undefined set failed: " + err.Error())
	}

	// multi-tenancy enabled, type label, undefined not defined
	cfg = Config{
		Enabled: true,
		Type:    "label",
		Label:   "test",
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate cfg with enabled: true, type label, undefined not defined failed: " + err.Error())
	}

	// multi-tenancy enabled, type label, label not defined, should err
	cfg = Config{
		Enabled: true,
		Type:    "label",
	}
	if err := cfg.Validate(); err == nil {
		t.Errorf("Validate cfg with enabled: true, type label, label not defined, should err, failed")
	}
}

func Test_InjectLabelForID(t *testing.T) {
	var ctx context.Context
	ctx, _ = context.WithCancel(context.Background())
	ctx = InjectLabelForID(ctx, "test1", "test2")
	if ctx.Value("useLabelAsOrgID") != "test1" && ctx.Value("useLabelAsOrgIDUndefined") != "test2" {
		t.Errorf("InjectLabelForID failed when injecting label and undefined")
	}

	ctx, _ = context.WithCancel(context.Background())
	if err := InjectLabelForID(ctx, "", "test2"); err != nil {
		t.Errorf("InjectLabelForID failed with injecting nil label, no err thrown")
	}

	ctx, _ = context.WithCancel(context.Background())
	if err := InjectLabelForID(ctx, "test1", ""); err != nil {
		t.Errorf("InjectLabelForID failed with injecting nil for undefined, no err thrown")
	}

	ctx, _ = context.WithCancel(context.Background())
	if err := InjectLabelForID(ctx, "", ""); err != nil {
		t.Errorf("InjectLabelForID failed with injecting nil label and undefined, no err thrown")
	}
}

func Test_GetLabelFromContext(t *testing.T) {
	var ctx context.Context
	ctx, _ = context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, interface{}("useLabelAsOrgID"), "test1")
	ctx = context.WithValue(ctx, interface{}("useLabelAsOrgIDUndefined"), "test2")
	label, undefined := GetLabelFromContext(ctx)
	if label != "test1" || undefined != "test2" {
		t.Errorf("GetLabelFromContext failed with label and undefined defined")
	}

	ctx, _ = context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, interface{}("useLabelAsOrgIDUndefined"), "test2")
	label, undefined = GetLabelFromContext(ctx)
	if label != "" && undefined != "" {
		t.Errorf("GetLabelFromContext failed with label not defined and undefined defined")
	}

	ctx, _ = context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, interface{}("useLabelAsOrgID"), "test1")
	label, undefined = GetLabelFromContext(ctx)
	if label != "" && undefined != "" {
		t.Errorf("GetLabelFromContext failed with label defined and undefined not defined")
	}

	ctx, _ = context.WithCancel(context.Background())
	label, undefined = GetLabelFromContext(ctx)
	if label != "" && undefined != "" {
		t.Errorf("GetLabelFromContext failed with label and undefined not defined")
	}
}

func Test_GetUserIDFromContextAndStringLabels(t *testing.T) {
	// label present in the list
	labels := "{testLabel=\"testValue\"}"
	ctx, _ := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, interface{}("useLabelAsOrgID"), "testLabel")
	ctx = context.WithValue(ctx, interface{}("useLabelAsOrgIDUndefined"), "test2")
	if value, err := GetUserIDFromContextAndStringLabels(ctx, labels); value != "testValue" && err == nil {
		t.Errorf("GetUserIDFromContextAndStringLabels failed with label in list, should return value for key")
	}

	// label not present in the list
	labels = "{someOtherLabel=\"someOtherValue\"}"
	ctx, _ = context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, interface{}("useLabelAsOrgID"), "testLabel")
	ctx = context.WithValue(ctx, interface{}("useLabelAsOrgIDUndefined"), "test2")
	if value, err := GetUserIDFromContextAndStringLabels(ctx, labels); value != "test2" && err == nil {
		t.Errorf("GetUserIDFromContextAndStringLabels failed with label not in list, should return value for undefined")
	}

	// auth being used
	labels = "{someOtherLabel=\"someOtherValue\"}"
	ctx, _ = context.WithCancel(context.Background())
	ctx = user.InjectOrgID(ctx, "test")
	if value, err := GetUserIDFromContextAndStringLabels(ctx, labels); value != "test" && err == nil {
		t.Errorf("GetUserIDFromContextAndStringLabels failed with auth, should return value for orgID")
	}

	// orgID and label/undefined not defined
	labels = "{someOtherLabel=\"someOtherValue\"}"
	ctx, _ = context.WithCancel(context.Background())
	if _, err := GetUserIDFromContextAndStringLabels(ctx, labels); err == nil {
		t.Errorf("GetUserIDFromContextAndStringLabels failed for undefined, should return err")
	}
}
