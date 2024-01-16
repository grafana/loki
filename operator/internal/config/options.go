package config

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
)

// OptionsAndFrom will use a supplied type and convert to Options
// any options already set on Options will be ignored, this is used to allow
// cli flags to override anything specified in the config file.
func OptionsAndFrom(o manager.Options, loader ControllerManagerConfiguration) (manager.Options, error) {
	newObj, err := loader.Complete()
	if err != nil {
		return o, err
	}

	o = setLeaderElectionConfig(o, newObj)

	if o.MetricsBindAddress == "" && newObj.Metrics.BindAddress != "" {
		o.MetricsBindAddress = newObj.Metrics.BindAddress
	}

	if o.HealthProbeBindAddress == "" && newObj.Health.HealthProbeBindAddress != "" {
		o.HealthProbeBindAddress = newObj.Health.HealthProbeBindAddress
	}

	//nolint:staticcheck
	if o.Port == 0 && newObj.Webhook.Port != nil {
		o.Port = *newObj.Webhook.Port
	}

	//nolint:staticcheck
	if o.WebhookServer == nil {
		o.WebhookServer = webhook.NewServer(webhook.Options{
			Port: o.Port,
		})
	}

	return o, nil
}

func setLeaderElectionConfig(o manager.Options, obj configv1.ControllerManagerConfigurationSpec) manager.Options {
	if obj.LeaderElection == nil {
		// The source does not have any configuration; noop
		return o
	}

	if !o.LeaderElection && obj.LeaderElection.LeaderElect != nil {
		o.LeaderElection = *obj.LeaderElection.LeaderElect
	}

	if o.LeaderElectionResourceLock == "" && obj.LeaderElection.ResourceLock != "" {
		o.LeaderElectionResourceLock = obj.LeaderElection.ResourceLock
	}

	if o.LeaderElectionNamespace == "" && obj.LeaderElection.ResourceNamespace != "" {
		o.LeaderElectionNamespace = obj.LeaderElection.ResourceNamespace
	}

	if o.LeaderElectionID == "" && obj.LeaderElection.ResourceName != "" {
		o.LeaderElectionID = obj.LeaderElection.ResourceName
	}

	if o.LeaseDuration == nil && !reflect.DeepEqual(obj.LeaderElection.LeaseDuration, metav1.Duration{}) {
		o.LeaseDuration = &obj.LeaderElection.LeaseDuration.Duration
	}

	if o.RenewDeadline == nil && !reflect.DeepEqual(obj.LeaderElection.RenewDeadline, metav1.Duration{}) {
		o.RenewDeadline = &obj.LeaderElection.RenewDeadline.Duration
	}

	if o.RetryPeriod == nil && !reflect.DeepEqual(obj.LeaderElection.RetryPeriod, metav1.Duration{}) {
		o.RetryPeriod = &obj.LeaderElection.RetryPeriod.Duration
	}

	return o
}
