package operator

import "os"

const inClusterNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// GetNamespace returns the namespace the operator is running in from
// the `/var/run/secrets/kubernetes.io/serviceaccount/namespace` file.
func GetNamespace() (string, error) {
	b, err := os.ReadFile(inClusterNamespaceFile)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
