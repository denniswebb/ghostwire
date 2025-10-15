package metrics

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CountDNATMappings returns the number of DNAT mappings recorded in the provided map file.
func CountDNATMappings(path string) (int, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return 0, nil
	}

	if err := validateDNATMapPath(cleanPath); err != nil {
		return 0, err
	}

	file, err := os.Open(cleanPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("open dnat map %s: %w", cleanPath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan dnat map %s: %w", cleanPath, err)
	}

	return count, nil
}

func validateDNATMapPath(path string) error {
	clean := filepath.Clean(path)
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part == ".." {
			return fmt.Errorf("dnat map path %q contains unsupported traversal component", path)
		}
	}
	return nil
}
