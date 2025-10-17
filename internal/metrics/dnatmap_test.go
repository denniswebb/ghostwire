package metrics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCountDNATMappings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write := func(tb testing.TB, name, content string, perm os.FileMode) string {
		tb.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), perm); err != nil {
			tb.Fatalf("failed to write %s: %v", path, err)
		}
		return path
	}

	tests := []struct {
		name        string
		path        string
		setup       func(t *testing.T) string
		wantCount   int
		expectError string
	}{
		{
			name: "valid mappings",
			setup: func(t *testing.T) string {
				content := "# ghostwire dnat map\nsvc-a 10.0.0.1 10.0.0.2\nsvc-b 10.0.0.3 10.0.0.4\n# trailing comment\nsvc-c 10.0.0.5 10.0.0.6\n"
				return write(t, "valid.map", content, 0o600)
			},
			wantCount: 3,
		},
		{
			name: "empty file",
			setup: func(t *testing.T) string {
				return write(t, "empty.map", "", 0o600)
			},
			wantCount: 0,
		},
		{
			name: "comments only",
			setup: func(t *testing.T) string {
				content := "# comment\n# another comment\n"
				return write(t, "comments.map", content, 0o600)
			},
			wantCount: 0,
		},
		{
			name: "blank lines ignored",
			setup: func(t *testing.T) string {
				content := "\nsvc-a 10.0.0.1 10.0.0.2\n\nsvc-b 10.0.0.3 10.0.0.4\n\n"
				return write(t, "blank.map", content, 0o600)
			},
			wantCount: 2,
		},
		{
			name:      "file not found",
			path:      filepath.Join(dir, "missing.map"),
			wantCount: 0,
		},
		{
			name:        "path traversal rejected",
			path:        "../etc/passwd",
			expectError: "contains unsupported traversal component",
		},
		{
			name: "permission denied",
			setup: func(t *testing.T) string {
				if os.Geteuid() == 0 {
					t.Skip("skipping permission denied scenario when running as root")
				}
				path := write(t, "restricted.map", "svc 10.0.0.1 10.0.0.2\n", 0o600)
				if err := os.Chmod(path, 0o000); err != nil {
					t.Fatalf("chmod failed: %v", err)
				}
				t.Cleanup(func() {
					_ = os.Chmod(path, 0o600)
				})
				return path
			},
			expectError: "open dnat map",
		},
		{
			name: "mixed content",
			setup: func(t *testing.T) string {
				content := "# header\n\nsvc-a 10.0.0.1 10.0.0.2\n# comment\nsvc-b 10.0.0.3 10.0.0.4\n   \nsvc-c 10.0.0.5 10.0.0.6\n"
				return write(t, "mixed.map", content, 0o600)
			},
			wantCount: 3,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := tc.path
			if tc.setup != nil {
				path = tc.setup(t)
			}

			count, err := CountDNATMappings(path)

			if tc.expectError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.expectError)
				}
				if !strings.Contains(err.Error(), tc.expectError) {
					t.Fatalf("expected error to contain %q, got %v", tc.expectError, err)
				}
				if count != 0 {
					t.Fatalf("expected count 0 on error, got %d", count)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if count != tc.wantCount {
				t.Fatalf("unexpected mapping count: got %d want %d", count, tc.wantCount)
			}
		})
	}
}
