package discovery

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"text/template"
)

var (
	templateCache sync.Map
)

const DefaultPreviewPattern = "{{name}}-preview"

type patternData struct {
	Name string
}

// ApplyPattern renders the preview service name using the configured template
// string. Templates are cached after the first parse to avoid repeated work.
func ApplyPattern(pattern string, serviceName string) (string, error) {
	tpl, err := loadTemplate(pattern)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, patternData{Name: serviceName}); err != nil {
		return "", fmt.Errorf("render preview pattern %q for service %q: %w", pattern, serviceName, err)
	}

	return buf.String(), nil
}

// DerivePreviewName resolves the preview service name using configured suffixes
// or, if they do not apply, the provided pattern-based fallback.
func DerivePreviewName(name, activeSuffix, previewSuffix, pattern string) (string, error) {
	if activeSuffix != "" && previewSuffix != "" && strings.HasSuffix(name, activeSuffix) {
		return strings.TrimSuffix(name, activeSuffix) + previewSuffix, nil
	}
	return ApplyPattern(pattern, name)
}

var namePlaceholder = regexp.MustCompile(`{{\s*name\s*}}`)

func loadTemplate(pattern string) (*template.Template, error) {
	if tpl, ok := templateCache.Load(pattern); ok {
		return tpl.(*template.Template), nil
	}

	normalized := namePlaceholder.ReplaceAllString(pattern, "{{.Name}}")

	tpl, err := template.New("svc_preview_pattern").Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("parse preview pattern %q: %w", pattern, err)
	}

	templateCache.Store(pattern, tpl)
	return tpl, nil
}
