package discovery

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

type mockRoundTripper struct {
	t         *testing.T
	namespace string
	status    int
	err       error
	list      *corev1.ServiceList
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}

	if req.Method != http.MethodGet {
		m.t.Fatalf("unexpected method %q", req.Method)
	}

	wantPath := fmt.Sprintf("/api/v1/namespaces/%s/services", m.namespace)
	if req.URL.Path != wantPath {
		m.t.Fatalf("unexpected path %q, want %q", req.URL.Path, wantPath)
	}

	if m.status == 0 {
		m.status = http.StatusOK
	}

	if m.status >= 400 {
		return &http.Response{
			StatusCode: m.status,
			Body:       io.NopCloser(strings.NewReader(`{"message":"error"}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    req,
		}, nil
	}

	if m.list == nil {
		m.list = &corev1.ServiceList{}
	}

	data := encodeServiceList(m.t, m.list)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(data)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    req,
	}, nil
}

func encodeServiceList(t *testing.T, list *corev1.ServiceList) []byte {
	t.Helper()

	codec := scheme.Codecs.LegacyCodec(corev1.SchemeGroupVersion)
	data, err := runtime.Encode(codec, list)
	if err != nil {
		t.Fatalf("encode service list: %v", err)
	}
	return data
}

func newTestClientset(t *testing.T, namespace string, list *corev1.ServiceList, status int, roundTripErr error) *kubernetes.Clientset {
	t.Helper()

	rt := &mockRoundTripper{
		t:         t,
		namespace: namespace,
		status:    status,
		err:       roundTripErr,
		list:      list,
	}

	httpClient := &http.Client{Transport: rt}
	cfg := &rest.Config{
		Host:    "https://example.com",
		APIPath: "/api",
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &schema.GroupVersion{Group: "", Version: "v1"},
			NegotiatedSerializer: serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs},
		},
	}

	clientset, err := kubernetes.NewForConfigAndClient(cfg, httpClient)
	if err != nil {
		t.Fatalf("create clientset: %v", err)
	}

	return clientset
}

func newService(name string, clusterIP string, ports []corev1.ServicePort, opts ...func(*corev1.Service)) corev1.Service {
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: clusterIP,
			Ports:     append([]corev1.ServicePort(nil), ports...),
		},
	}

	if clusterIP != "" && clusterIP != corev1.ClusterIPNone {
		svc.Spec.ClusterIPs = []string{clusterIP}
	}

	for _, opt := range opts {
		opt(&svc)
	}

	return svc
}

func withClusterIPs(ips ...string) func(*corev1.Service) {
	return func(svc *corev1.Service) {
		svc.Spec.ClusterIPs = append([]string(nil), ips...)
		if len(ips) > 0 {
			svc.Spec.ClusterIP = ips[0]
		}
	}
}

func withoutClusterIP() func(*corev1.Service) {
	return func(svc *corev1.Service) {
		svc.Spec.ClusterIP = ""
		svc.Spec.ClusterIPs = nil
	}
}

func makeServiceList(services ...corev1.Service) *corev1.ServiceList {
	list := &corev1.ServiceList{
		Items: make([]corev1.Service, len(services)),
	}
	for i := range services {
		list.Items[i] = services[i]
	}
	return list
}

func port(name string, number int32, proto corev1.Protocol) corev1.ServicePort {
	return corev1.ServicePort{
		Name:     name,
		Port:     number,
		Protocol: proto,
	}
}

func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	handler := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(handler), buf
}

func mappingKey(m ServiceMapping) string {
	return fmt.Sprintf("%s:%d/%s", m.ServiceName, m.Port, m.Protocol)
}

func assertMappings(t *testing.T, got []ServiceMapping, want []ServiceMapping) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("mappings len = %d, want %d; got: %#v", len(got), len(want), got)
	}

	gotMap := make(map[string]ServiceMapping, len(got))
	for _, m := range got {
		gotMap[mappingKey(m)] = m
	}

	for _, expected := range want {
		key := mappingKey(expected)
		actual, ok := gotMap[key]
		if !ok {
			t.Fatalf("expected mapping %s not found; got %#v", key, got)
		}

		if actual.ActiveClusterIP != expected.ActiveClusterIP || actual.PreviewClusterIP != expected.PreviewClusterIP || actual.Protocol != expected.Protocol {
			t.Fatalf("mapping %s mismatch: got %#v, want %#v", key, actual, expected)
		}
	}
}

func TestDiscover(t *testing.T) {
	t.Parallel()

	namespace := "ghostwire"

	tests := []struct {
		name         string
		services     []corev1.Service
		configure    func(*Config)
		statusCode   int
		roundTripErr error
		want         []ServiceMapping
		wantErr      bool
		clientsetNil bool
		wantLogs     []string
		absentLogs   []string
		customList   *corev1.ServiceList
		overrideNS   string
	}{
		{
			name: "happy path with multiple services",
			services: []corev1.Service{
				newService("orders", "10.0.0.10", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
					port("https", 443, corev1.ProtocolTCP),
				}),
				newService("orders-preview", "10.0.1.10", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
					port("https", 443, corev1.ProtocolTCP),
				}),
				newService("payment", "10.0.0.20", []corev1.ServicePort{
					port("http", 8080, corev1.ProtocolTCP),
				}),
				newService("payment-preview", "10.0.1.20", []corev1.ServicePort{
					port("http", 8080, corev1.ProtocolTCP),
				}),
			},
			want: []ServiceMapping{
				{ServiceName: "orders", Port: 80, Protocol: corev1.ProtocolTCP, ActiveClusterIP: "10.0.0.10", PreviewClusterIP: "10.0.1.10"},
				{ServiceName: "orders", Port: 443, Protocol: corev1.ProtocolTCP, ActiveClusterIP: "10.0.0.10", PreviewClusterIP: "10.0.1.10"},
				{ServiceName: "payment", Port: 8080, Protocol: corev1.ProtocolTCP, ActiveClusterIP: "10.0.0.20", PreviewClusterIP: "10.0.1.20"},
			},
		},
		{
			name: "no preview service",
			services: []corev1.Service{
				newService("users", "10.0.0.30", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
			},
			want:     nil,
			wantLogs: []string{"no preview service found"},
		},
		{
			name: "headless service skipped",
			services: []corev1.Service{
				newService("headless", corev1.ClusterIPNone, []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
				newService("headless-preview", "10.0.2.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
			},
			want:     nil,
			wantLogs: []string{"skipping service with invalid cluster IP"},
		},
		{
			name: "empty cluster ip skipped",
			services: []corev1.Service{
				newService("empty-ip", "", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
				newService("empty-ip-preview", "10.0.2.2", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
			},
			want:     nil,
			wantLogs: []string{"skipping service with invalid cluster IP"},
		},
		{
			name: "identical cluster ips skipped",
			services: []corev1.Service{
				newService("duplicate-ip", "10.0.3.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
				newService("duplicate-ip-preview", "10.0.3.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
			},
			want:     nil,
			wantLogs: []string{"skipping service with identical active and preview cluster IPs"},
		},
		{
			name: "service with no ports skipped",
			services: []corev1.Service{
				newService("no-ports", "10.0.4.1", nil),
				newService("no-ports-preview", "10.0.5.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
			},
			want:     nil,
			wantLogs: []string{"skipping service with no ports"},
		},
		{
			name: "port mismatch logs warning but keeps matches",
			services: []corev1.Service{
				newService("port-mismatch", "10.0.6.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
					port("admin", 8443, corev1.ProtocolTCP),
				}),
				newService("port-mismatch-preview", "10.0.7.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
			},
			want: []ServiceMapping{
				{ServiceName: "port-mismatch", Port: 80, Protocol: corev1.ProtocolTCP, ActiveClusterIP: "10.0.6.1", PreviewClusterIP: "10.0.7.1"},
			},
			wantLogs: []string{"preview service missing matching port"},
		},
		{
			name: "protocol mismatch skipped",
			services: []corev1.Service{
				newService("protocol-mismatch", "10.0.8.1", []corev1.ServicePort{
					port("dns", 53, corev1.ProtocolTCP),
				}),
				newService("protocol-mismatch-preview", "10.0.9.1", []corev1.ServicePort{
					port("dns", 53, corev1.ProtocolUDP),
				}),
			},
			want:     nil,
			wantLogs: []string{"preview service missing matching port"},
		},
		{
			name: "multiple ports all mapped",
			services: []corev1.Service{
				newService("api", "10.0.10.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
					port("https", 443, corev1.ProtocolTCP),
					port("grpc", 8080, corev1.ProtocolTCP),
				}),
				newService("api-preview", "10.0.11.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
					port("https", 443, corev1.ProtocolTCP),
					port("grpc", 8080, corev1.ProtocolTCP),
				}),
			},
			want: []ServiceMapping{
				{ServiceName: "api", Port: 80, Protocol: corev1.ProtocolTCP, ActiveClusterIP: "10.0.10.1", PreviewClusterIP: "10.0.11.1"},
				{ServiceName: "api", Port: 443, Protocol: corev1.ProtocolTCP, ActiveClusterIP: "10.0.10.1", PreviewClusterIP: "10.0.11.1"},
				{ServiceName: "api", Port: 8080, Protocol: corev1.ProtocolTCP, ActiveClusterIP: "10.0.10.1", PreviewClusterIP: "10.0.11.1"},
			},
		},
		{
			name: "ipv4 and ipv6 services",
			services: []corev1.Service{
				newService("ipv4", "10.1.0.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
				newService("ipv4-preview", "10.1.1.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
				newService("ipv6", "fd00::10", []corev1.ServicePort{
					port("http", 8080, corev1.ProtocolTCP),
				}, withClusterIPs("fd00::10")),
				newService("ipv6-preview", "fd00::20", []corev1.ServicePort{
					port("http", 8080, corev1.ProtocolTCP),
				}, withClusterIPs("fd00::20")),
			},
			want: []ServiceMapping{
				{ServiceName: "ipv4", Port: 80, Protocol: corev1.ProtocolTCP, ActiveClusterIP: "10.1.0.1", PreviewClusterIP: "10.1.1.1"},
				{ServiceName: "ipv6", Port: 8080, Protocol: corev1.ProtocolTCP, ActiveClusterIP: "fd00::10", PreviewClusterIP: "fd00::20"},
			},
		},
		{
			name: "suffix based pairing",
			services: []corev1.Service{
				newService("orders-active", "10.2.0.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
				newService("orders-preview", "10.2.1.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
			},
			configure: func(cfg *Config) {
				cfg.ActiveSuffix = "-active"
				cfg.PreviewSuffix = "-preview"
			},
			want: []ServiceMapping{
				{ServiceName: "orders-active", Port: 80, Protocol: corev1.ProtocolTCP, ActiveClusterIP: "10.2.0.1", PreviewClusterIP: "10.2.1.1"},
			},
		},
		{
			name: "pattern based pairing",
			services: []corev1.Service{
				newService("accounts", "10.3.0.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
				newService("preview-accounts", "10.3.1.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
			},
			configure: func(cfg *Config) {
				cfg.PreviewPattern = "preview-{{name}}"
				cfg.PreviewSuffix = ""
			},
			want: []ServiceMapping{
				{ServiceName: "accounts", Port: 80, Protocol: corev1.ProtocolTCP, ActiveClusterIP: "10.3.0.1", PreviewClusterIP: "10.3.1.1"},
			},
		},
		{
			name: "preview services skipped as base",
			services: []corev1.Service{
				newService("orders", "10.4.0.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
				newService("orders-preview", "10.4.1.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
				newService("legacy-preview", "10.4.2.1", []corev1.ServicePort{
					port("http", 80, corev1.ProtocolTCP),
				}),
			},
			want: []ServiceMapping{
				{ServiceName: "orders", Port: 80, Protocol: corev1.ProtocolTCP, ActiveClusterIP: "10.4.0.1", PreviewClusterIP: "10.4.1.1"},
			},
			wantLogs: []string{"skipping preview service as base"},
		},
		{
			name:         "nil clientset errors",
			clientsetNil: true,
			wantErr:      true,
		},
		{
			name: "empty namespace errors",
			configure: func(cfg *Config) {
				cfg.Namespace = ""
			},
			services: []corev1.Service{},
			wantErr:  true,
		},
		{
			name: "empty preview pattern errors",
			configure: func(cfg *Config) {
				cfg.PreviewPattern = ""
			},
			services: []corev1.Service{},
			wantErr:  true,
		},
		{
			name:       "kubernetes api error",
			services:   []corev1.Service{},
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var clientset *kubernetes.Clientset
			ns := namespace
			if tc.overrideNS != "" {
				ns = tc.overrideNS
			}

			if !tc.clientsetNil {
				list := tc.customList
				if list == nil {
					list = makeServiceList(tc.services...)
				}
				clientset = newTestClientset(t, ns, list, tc.statusCode, tc.roundTripErr)
			}

			logger, buf := newTestLogger()

			cfg := Config{
				Clientset:      clientset,
				Namespace:      ns,
				PreviewPattern: DefaultPreviewPattern,
				PreviewSuffix:  "-preview",
			}

			if tc.configure != nil {
				tc.configure(&cfg)
			}

			got, err := Discover(context.Background(), cfg, logger)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Discover expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("Discover returned error: %v", err)
			}

			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("expected no mappings, got %#v", got)
				}
			} else {
				assertMappings(t, got, tc.want)
			}

			logOutput := buf.String()
			for _, substring := range tc.wantLogs {
				if !strings.Contains(logOutput, substring) {
					t.Fatalf("expected log output to contain %q, got %q", substring, logOutput)
				}
			}
			for _, substring := range tc.absentLogs {
				if strings.Contains(logOutput, substring) {
					t.Fatalf("expected log output to exclude %q, got %q", substring, logOutput)
				}
			}
		})
	}
}
