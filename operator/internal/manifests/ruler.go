package manifests

import (
	"fmt"
	"path"

	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildRuler returns a list of k8s objects for Loki Stack Ruler
func BuildRuler(opts Options) ([]client.Object, error) {
	statefulSet := NewRulerStatefulSet(opts)
	if opts.Flags.EnableTLSServiceMonitorConfig {
		if err := configureRulerServiceMonitorPKI(statefulSet, opts.Name); err != nil {
			return nil, err
		}
	}

	return []client.Object{
		statefulSet,
		NewRulerGRPCService(opts),
		NewRulerHTTPService(opts),
	}, nil
}

// NewRulerStatefulSet creates a statefulset object for an ruler
func NewRulerStatefulSet(opts Options) *appsv1.StatefulSet {
	podSpec := corev1.PodSpec{
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
					ConfigMap: &corev1.ConfigMapVolumeSource{
						DefaultMode: &defaultConfigMapMode,
						LocalObjectReference: corev1.LocalObjectReference{
							Name: RulesConfigMapName(opts.Name),
						},
						Items: ruleVolumeItems(opts.Tenants.Configs),
					},
				},
			},
		},
		Containers: []corev1.Container{
			{
				Image: opts.Image,
				Name:  "loki-ruler",
				Resources: corev1.ResourceRequirements{
					Limits:   opts.ResourceRequirements.Ruler.Limits,
					Requests: opts.ResourceRequirements.Ruler.Requests,
				},
				Args: []string{
					"-target=ruler",
					fmt.Sprintf("-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiConfigFileName)),
					fmt.Sprintf("-runtime-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiRuntimeConfigFileName)),
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
						Name:      rulesStorageVolumeName,
						ReadOnly:  false,
						MountPath: rulesStorageDirectory,
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

	l := ComponentLabels(LabelRulerComponent, opts.Name)
	a := commonAnnotations(opts.ConfigSHA1)

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
			RevisionHistoryLimit: pointer.Int32Ptr(10),
			Replicas:             pointer.Int32Ptr(opts.Stack.Template.Ruler.Replicas),
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
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: opts.ResourceRequirements.Ruler.PVCSize,
							},
						},
						StorageClassName: pointer.StringPtr(opts.Stack.StorageClassName),
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
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: opts.ResourceRequirements.WALStorage.PVCSize,
							},
						},
						StorageClassName: pointer.StringPtr(opts.Stack.StorageClassName),
						VolumeMode:       &volumeFileSystemMode,
					},
				},
			},
		},
	}
}

// NewRulerGRPCService creates a k8s service for the ruler GRPC endpoint
func NewRulerGRPCService(opts Options) *corev1.Service {
	l := ComponentLabels(LabelRulerComponent, opts.Name)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameRulerGRPC(opts.Name),
			Labels: l,
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
			Selector: l,
		},
	}
}

// NewRulerHTTPService creates a k8s service for the ruler HTTP endpoint
func NewRulerHTTPService(opts Options) *corev1.Service {
	serviceName := serviceNameRulerHTTP(opts.Name)
	l := ComponentLabels(LabelRulerComponent, opts.Name)
	a := serviceAnnotations(serviceName, opts.Flags.EnableCertificateSigningService)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Labels:      l,
			Annotations: a,
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
			Selector: l,
		},
	}
}

func configureRulerServiceMonitorPKI(statefulSet *appsv1.StatefulSet, stackName string) error {
	serviceName := serviceNameRulerHTTP(stackName)
	return configureServiceMonitorPKI(&statefulSet.Spec.Template.Spec, serviceName)
}

func ruleVolumeItems(tenants map[string]TenantConfig) []corev1.KeyToPath {
	var items []corev1.KeyToPath

	for id, tenant := range tenants {
		for _, rule := range tenant.RuleFiles {
			items = append(items, corev1.KeyToPath{
				Key:  rule,
				Path: fmt.Sprintf("%s/%s", id, rule),
			})
		}
	}

	return items
}
