package cmd

import (
	"fmt"
	"strings"

	"mineru-cli/internal/markdown"
)

// validateSplitLevel rejects anything but 0 (no split), 1, or 2.
func validateSplitLevel(level int) error {
	if level < 0 || level > 2 {
		return fmt.Errorf("--split-level must be 0, 1, or 2 (got %d)", level)
	}
	return nil
}

// applySplitLevel splits every markdown file in written into one file per
// heading at the given level, replacing each split file's entry in written
// with its resulting section files. level == 0 is a no-op.
func applySplitLevel(written []string, level int) ([]string, error) {
	if level == 0 {
		return written, nil
	}
	result := make([]string, 0, len(written))
	for _, path := range written {
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			result = append(result, path)
			continue
		}
		sections, ok, err := markdown.SplitFile(path, level)
		if err != nil {
			return result, fmt.Errorf("split %s: %w", path, err)
		}
		if !ok {
			result = append(result, path)
			continue
		}
		result = append(result, sections...)
	}
	return result, nil
}
