package k8s

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PodLabelReader fetches labels from a Pod in the cluster.
type PodLabelReader struct {
	client    kubernetes.Interface
	namespace string
	podName   string
}

// NewPodLabelReader constructs a PodLabelReader for the given pod reference.
func NewPodLabelReader(client kubernetes.Interface, namespace, podName string) *PodLabelReader {
	return &PodLabelReader{
		client:    client,
		namespace: namespace,
		podName:   podName,
	}
}

// GetLabel returns the value of the requested label on the configured Pod. When the label
// is missing it returns an empty string and nil error so callers can treat absence as a state.
func (r *PodLabelReader) GetLabel(ctx context.Context, labelKey string) (string, error) {
	pod, err := r.client.CoreV1().Pods(r.namespace).Get(ctx, r.podName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("pod %s/%s not found while reading label %q: %w", r.namespace, r.podName, labelKey, err)
		}
		return "", fmt.Errorf("get pod %s/%s for label %q: %w", r.namespace, r.podName, labelKey, err)
	}

	if pod.Labels == nil {
		return "", nil
	}

	value, ok := pod.Labels[labelKey]
	if !ok {
		return "", nil
	}

	return value, nil
}
