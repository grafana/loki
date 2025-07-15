package manifests

import (
	"strings"

	"github.com/imdario/mergo"
	corev1 "k8s.io/api/core/v1"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

const (
	httpProxyKey  = "HTTP_PROXY"
	httpsProxyKey = "HTTPS_PROXY"
	noProxyKey    = "NO_PROXY"
)

var proxyEnvNames = []string{
	httpProxyKey,
	strings.ToLower(httpProxyKey),
	httpsProxyKey,
	strings.ToLower(httpsProxyKey),
	noProxyKey,
	strings.ToLower(noProxyKey),
}

func configureProxyEnv(pod *corev1.PodSpec, opts Options) error {
	for _, envVar := range proxyEnvNames {
		resetProxyVar(pod, envVar)
	}

	proxySpec := opts.Stack.Proxy
	if proxySpec == nil {
		return nil
	}

	src := corev1.Container{
		Env: toEnvVars(proxySpec),
	}

	for i, dst := range pod.Containers {
		if err := mergo.Merge(&dst, src, mergo.WithAppendSlice); err != nil {
			return err
		}
		pod.Containers[i] = dst
	}

	return nil
}

func resetProxyVar(podSpec *corev1.PodSpec, name string) {
	for i, container := range podSpec.Containers {
		found, index := findEnvVar(name, container.Env)
		if found {
			podSpec.Containers[i].Env = append(podSpec.Containers[i].Env[:index], podSpec.Containers[i].Env[index+1:]...)
		}
	}
}

func findEnvVar(name string, envVars []corev1.EnvVar) (bool, int) {
	for i, env := range envVars {
		if env.Name == name || env.Name == strings.ToLower(name) {
			return true, i
		}
	}
	return false, 0
}

func toEnvVars(proxySpec *lokiv1.ClusterProxy) []corev1.EnvVar {
	var envVars []corev1.EnvVar
	if proxySpec.HTTPProxy != "" {
		envVars = append(envVars,
			corev1.EnvVar{
				Name:  httpProxyKey,
				Value: proxySpec.HTTPProxy,
			},
			corev1.EnvVar{
				Name:  strings.ToLower(httpProxyKey),
				Value: proxySpec.HTTPProxy,
			},
		)
	}

	if proxySpec.HTTPSProxy != "" {
		envVars = append(envVars,
			corev1.EnvVar{
				Name:  httpsProxyKey,
				Value: proxySpec.HTTPSProxy,
			},
			corev1.EnvVar{
				Name:  strings.ToLower(httpsProxyKey),
				Value: proxySpec.HTTPSProxy,
			},
		)
	}

	if proxySpec.NoProxy != "" {
		envVars = append(envVars,
			corev1.EnvVar{
				Name:  noProxyKey,
				Value: proxySpec.NoProxy,
			},
			corev1.EnvVar{
				Name:  strings.ToLower(noProxyKey),
				Value: proxySpec.NoProxy,
			},
		)
	}

	return envVars
}
