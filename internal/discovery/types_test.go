package discovery

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestServiceMappingString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mapping ServiceMapping
		want    string
	}{
		{
			name:    "zero value",
			mapping: ServiceMapping{},
			want:    ":0/ -> active= preview=",
		},
		{
			name: "tcp mapping",
			mapping: ServiceMapping{
				ServiceName:      "orders",
				Port:             80,
				Protocol:         corev1.ProtocolTCP,
				ActiveClusterIP:  "10.0.0.10",
				PreviewClusterIP: "10.0.1.10",
			},
			want: "orders:80/TCP -> active=10.0.0.10 preview=10.0.1.10",
		},
		{
			name: "udp mapping",
			mapping: ServiceMapping{
				ServiceName:      "dns",
				Port:             53,
				Protocol:         corev1.ProtocolUDP,
				ActiveClusterIP:  "10.0.0.53",
				PreviewClusterIP: "10.0.1.53",
			},
			want: "dns:53/UDP -> active=10.0.0.53 preview=10.0.1.53",
		},
		{
			name: "sctp mapping with ipv6 and max port",
			mapping: ServiceMapping{
				ServiceName:      "stream",
				Port:             65535,
				Protocol:         corev1.ProtocolSCTP,
				ActiveClusterIP:  "fd00::1",
				PreviewClusterIP: "fd00::2",
			},
			want: "stream:65535/SCTP -> active=fd00::1 preview=fd00::2",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.mapping.String(); got != tc.want {
				t.Fatalf("ServiceMapping.String() = %q, want %q", got, tc.want)
			}
		})
	}
}
