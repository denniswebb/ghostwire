package discovery

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// NewInClusterClient creates a typed Kubernetes client using the pod's
// service account credentials. The caller must ensure the service account
// has RBAC permissions to list Services in the target namespace.
func NewInClusterClient() (*kubernetes.Clientset, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("build in-cluster k8s config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create k8s clientset: %w", err)
	}

	return clientset, nil
}
