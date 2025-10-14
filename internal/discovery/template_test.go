package discovery

import (
	"testing"
)

func TestApplyPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		service string
		want    string
		wantErr bool
	}{
		{
			name:    "default pattern simple name",
			pattern: DefaultPreviewPattern,
			service: "orders",
			want:    "orders-preview",
		},
		{
			name:    "default pattern hyphenated name",
			pattern: DefaultPreviewPattern,
			service: "payment-api",
			want:    "payment-api-preview",
		},
		{
			name:    "default pattern numeric suffix",
			pattern: DefaultPreviewPattern,
			service: "svc-v2",
			want:    "svc-v2-preview",
		},
		{
			name:    "custom suffix pattern",
			pattern: "{{name}}-canary",
			service: "orders",
			want:    "orders-canary",
		},
		{
			name:    "custom prefix pattern",
			pattern: "preview-{{name}}",
			service: "orders",
			want:    "preview-orders",
		},
		{
			name:    "identity pattern",
			pattern: "{{name}}",
			service: "orders",
			want:    "orders",
		},
		{
			name:    "empty service name",
			pattern: DefaultPreviewPattern,
			service: "",
			want:    "-preview",
		},
		{
			name:    "empty pattern",
			pattern: "",
			service: "orders",
			want:    "",
		},
		{
			name:    "invalid template syntax",
			pattern: "{{name",
			service: "orders",
			wantErr: true,
		},
		{
			name:    "missing field execution error",
			pattern: "{{preview}}-svc",
			service: "orders",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ApplyPattern(tc.pattern, tc.service)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ApplyPattern(%q, %q) expected error", tc.pattern, tc.service)
				}
				return
			}

			if err != nil {
				t.Fatalf("ApplyPattern(%q, %q) returned error: %v", tc.pattern, tc.service, err)
			}

			if got != tc.want {
				t.Fatalf("ApplyPattern(%q, %q) = %q, want %q", tc.pattern, tc.service, got, tc.want)
			}
		})
	}
}

func TestDerivePreviewName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		service       string
		activeSuffix  string
		previewSuffix string
		pattern       string
		want          string
		wantErr       bool
	}{
		{
			name:          "suffix match",
			service:       "orders-active",
			activeSuffix:  "-active",
			previewSuffix: "-preview",
			pattern:       DefaultPreviewPattern,
			want:          "orders-preview",
		},
		{
			name:          "suffix mismatch falls back to pattern",
			service:       "orders",
			activeSuffix:  "-active",
			previewSuffix: "-preview",
			pattern:       DefaultPreviewPattern,
			want:          "orders-preview",
		},
		{
			name:          "empty suffix fallback",
			service:       "orders",
			activeSuffix:  "",
			previewSuffix: "",
			pattern:       "preview-{{name}}",
			want:          "preview-orders",
		},
		{
			name:          "service equals suffix",
			service:       "-active",
			activeSuffix:  "-active",
			previewSuffix: "-preview",
			pattern:       DefaultPreviewPattern,
			want:          "-preview",
		},
		{
			name:          "invalid fallback pattern",
			service:       "orders",
			activeSuffix:  "-active",
			previewSuffix: "-preview",
			pattern:       "{{name",
			wantErr:       true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := DerivePreviewName(tc.service, tc.activeSuffix, tc.previewSuffix, tc.pattern)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("DerivePreviewName expected error for %q", tc.service)
				}
				return
			}

			if err != nil {
				t.Fatalf("DerivePreviewName returned error for %q: %v", tc.service, err)
			}

			if got != tc.want {
				t.Fatalf("DerivePreviewName(%q) = %q, want %q", tc.service, got, tc.want)
			}
		})
	}
}

func TestLoadTemplateCaching(t *testing.T) {
	t.Parallel()

	pattern := "{{name}}-cached"

	tpl1, err := loadTemplate(pattern)
	if err != nil {
		t.Fatalf("first loadTemplate returned error: %v", err)
	}

	tpl2, err := loadTemplate(pattern)
	if err != nil {
		t.Fatalf("second loadTemplate returned error: %v", err)
	}

	if tpl1 != tpl2 {
		t.Fatalf("expected cached template pointers to match, got %p and %p", tpl1, tpl2)
	}
}
