package controllers

import (
	"github.com/go-logr/logr"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
)

const (
	AlertingRuleCtrlName        = "alertingrule"
	CertRotationCtrlName        = "certrotation"
	DashboardsCtrlName          = "dashboard"
	LokiStackCtrlName           = "lokistack"
	LokiStackZoneLabelsCtrlName = "lokistack-zone-labels"
	RecordingRuleCtrlName       = "recordingrule"
	RulerConfigCtrlName         = "rulerconfig"
)

type LogContructorType func(request *reconcile.Request) logr.Logger

func genericLogConstructor(log logr.Logger, name string) LogContructorType {
	return func(_ *reconcile.Request) logr.Logger {
		log := log
		return log.WithValues("controller", name)
	}
}

func lokiStackLogConstructor(log logr.Logger, name string) LogContructorType {
	return func(req *reconcile.Request) logr.Logger {
		log := log
		l := log.WithValues("controller", name)

		if req != nil {
			l = l.WithValues(
				"controllerGroup", lokiv1.GroupVersion.Group,
				"controllerKind", "LokiStack",
				"lokistack", klog.KRef(req.Namespace, req.Name),
			)
		}

		return l
	}
}

func logWithLokiStackRef(log logr.Logger, req ctrl.Request, name string) logr.Logger {
	return log.WithValues(
		"controller", name,
		"lokistack", klog.KRef(req.Namespace, req.Name),
	)
}
