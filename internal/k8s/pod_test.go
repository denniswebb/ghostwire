package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestPodLabelReader_GetLabel(t *testing.T) {
	t.Parallel()

	baseCtx := context.Background()

	type testCase struct {
		name        string
		pod         *corev1.Pod
		labelKey    string
		prepare     func(t *testing.T, objects []runtime.Object, reader **PodLabelReader)
		expected    string
		expectError string
	}

	tests := []testCase{
		{
			name:     "happy path returns label value",
			pod:      newTestPod(map[string]string{"role": "active"}),
			labelKey: "role",
			expected: "active",
		},
		{
			name:     "label missing returns empty string",
			pod:      newTestPod(map[string]string{"role": "active"}),
			labelKey: "missing",
			expected: "",
		},
		{
			name:        "pod not found returns contextual error",
			pod:         nil,
			labelKey:    "role",
			expectError: "pod ghostwire/ghostwire-watcher not found while reading label \"role\"",
		},
		{
			name:     "api error wrapped with context",
			pod:      newTestPod(map[string]string{"role": "active"}),
			labelKey: "role",
			prepare: func(t *testing.T, objects []runtime.Object, reader **PodLabelReader) {
				t.Helper()
				client := fake.NewSimpleClientset(objects...)
				client.PrependReactor("get", "pods", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("boom")
				})
				*reader = NewPodLabelReader(client, "ghostwire", "ghostwire-watcher")
			},
			expectError: "get pod ghostwire/ghostwire-watcher for label \"role\": boom",
		},
		{
			name:     "nil labels map returns empty value",
			pod:      newTestPod(nil),
			labelKey: "role",
			expected: "",
		},
		{
			name:     "empty label value allowed",
			pod:      newTestPod(map[string]string{"role": ""}),
			labelKey: "role",
			expected: "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var objects []runtime.Object
			if tc.pod != nil {
				objects = append(objects, tc.pod)
			}
			client := fake.NewSimpleClientset(objects...)

			reader := NewPodLabelReader(client, "ghostwire", "ghostwire-watcher")
			if tc.prepare != nil {
				tc.prepare(t, objects, &reader)
			}

			value, err := reader.GetLabel(baseCtx, tc.labelKey)

			if tc.expectError != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tc.expectError)
				}
				if value != "" {
					t.Fatalf("expected empty value on error, got %q", value)
				}
				if !containsString(err.Error(), tc.expectError) {
					t.Fatalf("expected error to contain %q, got %v", tc.expectError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if value != tc.expected {
				t.Fatalf("expected value %q, got %q", tc.expected, value)
			}
		})
	}
}

func newTestPod(labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ghostwire",
			Name:      "ghostwire-watcher",
			Labels:    labels,
		},
	}
}

func containsString(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
