package discovery

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// ServiceMapping represents a single port mapping between an active/base service
// and its preview variant. These mappings later drive DNAT rule creation.
type ServiceMapping struct {
	ServiceName      string
	Port             int32
	Protocol         corev1.Protocol
	ActiveClusterIP  string
	PreviewClusterIP string
}

func (m ServiceMapping) String() string {
	return fmt.Sprintf(
		"%s:%d/%s -> active=%s preview=%s",
		m.ServiceName,
		m.Port,
		string(m.Protocol),
		m.ActiveClusterIP,
		m.PreviewClusterIP,
	)
}
