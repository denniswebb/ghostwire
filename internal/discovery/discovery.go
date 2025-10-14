package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Config captures the inputs required for service discovery.
type Config struct {
	Clientset      *kubernetes.Clientset
	Namespace      string
	PreviewPattern string
	ActiveSuffix   string
	PreviewSuffix  string
}

// Discover lists services in the configured namespace, pairing base services
// with their preview counterparts using the provided name pattern.
func Discover(ctx context.Context, cfg Config, logger *slog.Logger) ([]ServiceMapping, error) {
	if cfg.Clientset == nil {
		return nil, fmt.Errorf("kubernetes clientset must be provided")
	}
	if cfg.Namespace == "" {
		return nil, fmt.Errorf("namespace must be provided")
	}
	if cfg.PreviewPattern == "" {
		return nil, fmt.Errorf("preview pattern must be provided")
	}
	if logger == nil {
		logger = slog.Default()
	}

	serviceList, err := cfg.Clientset.CoreV1().Services(cfg.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services in namespace %q: %w", cfg.Namespace, err)
	}

	serviceMap := make(map[string]*corev1.Service, len(serviceList.Items))
	for i := range serviceList.Items {
		svc := &serviceList.Items[i]
		serviceMap[svc.Name] = svc
	}

	mappings := make([]ServiceMapping, 0)

	for i := range serviceList.Items {
		svc := &serviceList.Items[i]

		if cfg.PreviewPattern == DefaultPreviewPattern && cfg.PreviewSuffix == "-preview" && strings.HasSuffix(svc.Name, cfg.PreviewSuffix) {
			logger.Debug("skipping preview service as base", slog.String("service", svc.Name))
			continue
		}

		previewName, err := DerivePreviewName(svc.Name, cfg.ActiveSuffix, cfg.PreviewSuffix, cfg.PreviewPattern)
		if err != nil {
			return nil, err
		}

		previewSvc, ok := serviceMap[previewName]
		if !ok {
			logger.Debug("no preview service found", slog.String("service", svc.Name), slog.String("expected_preview", previewName))
			continue
		}

		activeIP := clusterIP(svc)
		previewIP := clusterIP(previewSvc)

		if !isValidClusterIP(activeIP) {
			logger.Warn("skipping service with invalid cluster IP", slog.String("service", svc.Name), slog.String("cluster_ip", activeIP))
			continue
		}
		if !isValidClusterIP(previewIP) {
			logger.Warn("skipping service with invalid preview cluster IP", slog.String("service", svc.Name), slog.String("preview_service", previewName), slog.String("cluster_ip", previewIP))
			continue
		}
		if activeIP == previewIP {
			logger.Warn("skipping service with identical active and preview cluster IPs", slog.String("service", svc.Name), slog.String("preview_service", previewName), slog.String("cluster_ip", activeIP))
			continue
		}

		if len(svc.Spec.Ports) == 0 {
			logger.Warn("skipping service with no ports", slog.String("service", svc.Name))
			continue
		}

		previewPorts := buildNumericPortMap(previewSvc.Spec.Ports)

		for _, port := range svc.Spec.Ports {
			lookupKey := numericPortKey(port)
			previewPort, ok := previewPorts[lookupKey]
			if !ok {
				logger.Warn("preview service missing matching port", slog.String("service", svc.Name), slog.String("preview_service", previewName), slog.String("port_key", lookupKey))
				continue
			}

			if port.Protocol != previewPort.Protocol {
				logger.Warn("protocol mismatch between active and preview service", slog.String("service", svc.Name), slog.String("preview_service", previewName), slog.String("port_key", lookupKey), slog.String("active_protocol", string(port.Protocol)), slog.String("preview_protocol", string(previewPort.Protocol)))
				continue
			}

			if port.Name != "" && previewPort.Name != "" && port.Name != previewPort.Name {
				logger.Warn(
					"port name mismatch for numeric match",
					slog.String("service", svc.Name),
					slog.String("preview_service", previewName),
					slog.String("active_port_name", port.Name),
					slog.String("preview_port_name", previewPort.Name),
					slog.Int("port", int(port.Port)),
					slog.String("protocol", string(port.Protocol)),
				)
			}

			mapping := ServiceMapping{
				ServiceName:      svc.Name,
				Port:             port.Port,
				Protocol:         port.Protocol,
				ActiveClusterIP:  activeIP,
				PreviewClusterIP: previewIP,
			}

			logger.Info(
				"discovered preview mapping",
				slog.String("service", svc.Name),
				slog.String("preview_service", previewName),
				slog.Int("port", int(port.Port)),
				slog.String("protocol", string(port.Protocol)),
				slog.String("active_ip", activeIP),
				slog.String("preview_ip", previewIP),
			)

			mappings = append(mappings, mapping)
		}
	}

	return mappings, nil
}

func isValidClusterIP(ip string) bool {
	if ip == "" || ip == corev1.ClusterIPNone {
		return false
	}
	return true
}

func buildNumericPortMap(ports []corev1.ServicePort) map[string]corev1.ServicePort {
	result := make(map[string]corev1.ServicePort, len(ports))
	for _, port := range ports {
		result[numericPortKey(port)] = port
	}
	return result
}

func numericPortKey(port corev1.ServicePort) string {
	return fmt.Sprintf("%d/%s", port.Port, port.Protocol)
}

func clusterIP(svc *corev1.Service) string {
	if len(svc.Spec.ClusterIPs) > 0 {
		return svc.Spec.ClusterIPs[0]
	}
	return svc.Spec.ClusterIP
}
