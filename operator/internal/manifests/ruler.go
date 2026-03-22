package manifests

import (
	"fmt"
	"path"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

// BuildRuler returns a list of k8s objects for Loki Stack Ruler
func BuildRuler(opts Options) ([]client.Object, error) {
	statefulSet := NewRulerStatefulSet(opts)
	if opts.Gates.HTTPEncryption {
		if err := configureRulerHTTPServicePKI(statefulSet, opts); err != nil {
			return nil, err
		}
	}

	if err := storage.ConfigureStatefulSet(statefulSet, opts.ObjectStorage); err != nil {
		return nil, err
	}

	if opts.Gates.GRPCEncryption {
		if err := configureRulerGRPCServicePKI(statefulSet, opts); err != nil {
			return nil, err
		}
	}

	if opts.Gates.HTTPEncryption || opts.Gates.GRPCEncryption {
		caBundleName := signingCABundleName(opts.Name)
		if err := configureServiceCA(&statefulSet.Spec.Template.Spec, caBundleName); err != nil {
			return nil, err
		}
	}

	objs := []client.Object{}
	if opts.Stack.Tenants != nil {
		if err := configureRulerStatefulSetForMode(statefulSet, opts.Stack.Tenants.Mode); err != nil {
			return nil, err
		}

		objs = configureRulerObjsForMode(opts)
	}

	if opts.Gates.RestrictedPodSecurityStandard {
		if err := configurePodSpecForRestrictedStandard(&statefulSet.Spec.Template.Spec); err != nil {
			return nil, err
		}
	}

	if err := configureHashRingEnv(&statefulSet.Spec.Template.Spec, opts); err != nil {
		return nil, err
	}

	if err := configureProxyEnv(&statefulSet.Spec.Template.Spec, opts); err != nil {
		return nil, err
	}

	if err := configureReplication(&statefulSet.Spec.Template, opts.Stack.Replication, LabelRulerComponent, opts.Name); err != nil {
		return nil, err
	}

	return append(objs,
		statefulSet,
		NewRulerGRPCService(opts),
		NewRulerHTTPService(opts),
		NewRulerPodDisruptionBudget(opts),
	), nil
}

// NewRulerStatefulSet creates a StatefulSet object for a ruler
func NewRulerStatefulSet(opts Options) *appsv1.StatefulSet {
	var volumeProjections []corev1.VolumeProjection

	for _, name := range opts.RulesConfigMapNames {
		volumeProjections = append(volumeProjections, corev1.VolumeProjection{
			ConfigMap: &corev1.ConfigMapProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: name,
				},
				Items: ruleVolumeItems(name, opts.Tenants.Configs),
			},
		})
	}

	l := ComponentLabels(LabelRulerComponent, opts.Name)
	a := commonAnnotations(opts)
	podSpec := corev1.PodSpec{
		Affinity: configureAffinity(LabelRulerComponent, opts.Name, opts.Gates.DefaultNodeAffinity, opts.Stack.Template.Ruler),
		Volumes: []corev1.Volume{
			{
				Name: configVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						DefaultMode: &defaultConfigMapMode,
						LocalObjectReference: corev1.LocalObjectReference{
							Name: lokiConfigMapName(opts.Name),
						},
					},
				},
			},
			{
				Name: rulesStorageVolumeName,
				VolumeSource: corev1.VolumeSource{
					Projected: &corev1.ProjectedVolumeSource{
						Sources: volumeProjections,
					},
				},
			},
		},
		Containers: []corev1.Container{
			{
				Image: opts.Image,
				Name:  rulerContainerName,
				Resources: corev1.ResourceRequirements{
					Limits:   opts.ResourceRequirements.Ruler.Limits,
					Requests: opts.ResourceRequirements.Ruler.Requests,
				},
				Args: []string{
					"-target=ruler",
					fmt.Sprintf("-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiConfigFileName)),
					fmt.Sprintf("-runtime-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiRuntimeConfigFileName)),
					"-config.expand-env=true",
				},
				ReadinessProbe: lokiReadinessProbe(),
				LivenessProbe:  lokiLivenessProbe(),
				Ports: []corev1.ContainerPort{
					{
						Name:          lokiHTTPPortName,
						ContainerPort: httpPort,
						Protocol:      protocolTCP,
					},
					{
						Name:          lokiGRPCPortName,
						ContainerPort: grpcPort,
						Protocol:      protocolTCP,
					},
					{
						Name:          lokiGossipPortName,
						ContainerPort: gossipPort,
						Protocol:      protocolTCP,
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      configVolumeName,
						ReadOnly:  false,
						MountPath: config.LokiConfigMountDir,
					},
					{
						Name:      walVolumeName,
						ReadOnly:  false,
						MountPath: walDirectory,
					},
					{
						Name:      storageVolumeName,
						ReadOnly:  false,
						MountPath: dataDirectory,
					},
					{
						Name:      rulesStorageVolumeName,
						ReadOnly:  false,
						MountPath: rulesStorageDirectory,
					},
				},
				TerminationMessagePath:   "/dev/termination-log",
				TerminationMessagePolicy: "File",
				ImagePullPolicy:          "IfNotPresent",
			},
		},
	}

	if opts.Stack.Template != nil && opts.Stack.Template.Ruler != nil {
		podSpec.Tolerations = opts.Stack.Template.Ruler.Tolerations
		podSpec.NodeSelector = opts.Stack.Template.Ruler.NodeSelector
	}

	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   RulerName(opts.Name),
			Labels: l,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy: appsv1.ParallelPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			RevisionHistoryLimit: ptr.To(defaultRevHistoryLimit),
			Replicas:             ptr.To(opts.Stack.Template.Ruler.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-ruler-%s", opts.Name),
					Labels:      labels.Merge(l, GossipLabels()),
					Annotations: a,
				},
				Spec: podSpec,
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Labels: l,
						Name:   storageVolumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							// TODO: should we verify that this is possible with the given storage class first?
							corev1.ReadWriteOnce,
						},
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: opts.ResourceRequirements.Ruler.PVCSize,
							},
						},
						StorageClassName: ptr.To(opts.Stack.StorageClassName),
						VolumeMode:       &volumeFileSystemMode,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Labels: l,
						Name:   walVolumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							// TODO: should we verify that this is possible with the given storage class first?
							corev1.ReadWriteOnce,
						},
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: opts.ResourceRequirements.WALStorage.PVCSize,
							},
						},
						StorageClassName: ptr.To(opts.Stack.StorageClassName),
						VolumeMode:       &volumeFileSystemMode,
					},
				},
			},
		},
	}
}

// NewRulerGRPCService creates a k8s service for the ruler GRPC endpoint
func NewRulerGRPCService(opts Options) *corev1.Service {
	serviceName := serviceNameRulerGRPC(opts.Name)
	labels := ComponentLabels(LabelRulerComponent, opts.Name)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceName,
			Labels: labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{
					Name:       lokiGRPCPortName,
					Port:       grpcPort,
					Protocol:   protocolTCP,
					TargetPort: intstr.IntOrString{IntVal: grpcPort},
				},
			},
			Selector: labels,
		},
	}
}

// NewRulerHTTPService creates a k8s service for the ruler HTTP endpoint
func NewRulerHTTPService(opts Options) *corev1.Service {
	serviceName := serviceNameRulerHTTP(opts.Name)
	labels := ComponentLabels(LabelRulerComponent, opts.Name)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceName,
			Labels: labels,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       lokiHTTPPortName,
					Port:       httpPort,
					Protocol:   protocolTCP,
					TargetPort: intstr.IntOrString{IntVal: httpPort},
				},
			},
			Selector: labels,
		},
	}
}

func configureRulerHTTPServicePKI(statefulSet *appsv1.StatefulSet, opts Options) error {
	serviceName := serviceNameRulerHTTP(opts.Name)
	return configureHTTPServicePKI(&statefulSet.Spec.Template.Spec, serviceName)
}

func configureRulerGRPCServicePKI(sts *appsv1.StatefulSet, opts Options) error {
	serviceName := serviceNameRulerGRPC(opts.Name)
	return configureGRPCServicePKI(&sts.Spec.Template.Spec, serviceName)
}

func configureRulerStatefulSetForMode(ss *appsv1.StatefulSet, mode lokiv1.ModeType) error {
	switch mode {
	case lokiv1.Static, lokiv1.Dynamic:
		return nil // nothing to configure
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		bundleName := alertmanagerSigningCABundleName(ss.Name)
		monitorServerName := fqdn(openshift.MonitoringSVCMain, openshift.MonitoringNS)
		return openshift.ConfigureRulerStatefulSet(
			ss,
			bundleName,
			BearerTokenFile,
			alertmanagerUpstreamCADir(),
			alertmanagerUpstreamCAPath(),
			monitorServerName,
			rulerContainerName,
		)
	}

	return nil
}

func configureRulerObjsForMode(opts Options) []client.Object {
	openShiftObjs := []client.Object{}

	switch opts.Stack.Tenants.Mode {
	case lokiv1.Static, lokiv1.Dynamic:
		// nothing to configure
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		openShiftObjs = openshift.BuildRulerObjects(opts.OpenShiftOptions)
	}

	return openShiftObjs
}

func ruleVolumeItems(configMapName string, tenants map[string]TenantConfig) []corev1.KeyToPath {
	var items []corev1.KeyToPath

	for tenantID, tenant := range tenants {
		for _, rule := range tenant.RuleFiles {
			shardName := extractRuleNameComponents(rule).cmName
			if shardName == configMapName {
				filename := extractRuleNameComponents(rule).filename
				items = append(items, corev1.KeyToPath{
					Key:  filename,
					Path: fmt.Sprintf("%s/%s", tenantID, filename),
				})
			}
		}
	}

	return items
}

// NewRulerPodDisruptionBudget returns a PodDisruptionBudget for the LokiStack ruler pods.
func NewRulerPodDisruptionBudget(opts Options) *policyv1.PodDisruptionBudget {
	l := ComponentLabels(LabelRulerComponent, opts.Name)

	ma := intstr.FromInt(1)

	return &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PodDisruptionBudget",
			APIVersion: policyv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    l,
			Name:      RulerName(opts.Name),
			Namespace: opts.Namespace,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: l,
			},
			MinAvailable: &ma,
		},
	}
}
