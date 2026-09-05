// Package markdown post-processes a downloaded MinerU markdown file,
// optionally splitting it into one file per chapter heading.
package markdown

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// headingPattern matches an ATX heading line, capturing its run of '#'
// characters and the heading text.
var headingPattern = regexp.MustCompile(`^(#{1,6})\s+(.*\S)\s*$`)

// SplitFile splits the markdown file at path into one file per heading found
// at the given level (1 = "#", 2 = "##"), named after that heading's text,
// and removes the original file. Files are written alongside the original,
// so relative links (e.g. to an "images/" directory) keep working unchanged.
//
// Content before the first matching heading (if any, e.g. a document title
// preceding the first "##" section) is folded into the first section rather
// than dropped.
//
// If level is <= 0, or no heading at that level exists, the file is left
// untouched and ok is false.
func SplitFile(path string, level int) (written []string, ok bool, err error) {
	if level <= 0 {
		return nil, false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}

	sections := splitSections(string(raw), level)
	if len(sections) < 2 {
		return nil, false, nil
	}

	dir := filepath.Dir(path)
	counts := map[string]int{}
	for _, sec := range sections {
		name := uniqueFilename(sanitizeFilename(sec.title), counts)
		dest := filepath.Join(dir, name)
		if err := os.WriteFile(dest, []byte(sec.body), 0o644); err != nil {
			return written, false, fmt.Errorf("write %s: %w", dest, err)
		}
		written = append(written, dest)
	}

	if err := os.Remove(path); err != nil {
		return written, true, fmt.Errorf("remove original %s: %w", path, err)
	}
	return written, true, nil
}

type section struct {
	title string
	body  string
}

// splitSections splits content into one section per heading at exactly the
// given level.
func splitSections(content string, level int) []section {
	lines := strings.Split(content, "\n")
	prefix := strings.Repeat("#", level)

	var starts []int
	var titles []string
	for i, line := range lines {
		m := headingPattern.FindStringSubmatch(line)
		if m == nil || m[1] != prefix {
			continue
		}
		starts = append(starts, i)
		titles = append(titles, m[2])
	}
	if len(starts) == 0 {
		return nil
	}

	sections := make([]section, 0, len(starts))
	for i, start := range starts {
		end := len(lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		body := strings.Join(lines[start:end], "\n")
		if i == 0 && start > 0 {
			front := strings.TrimRight(strings.Join(lines[:start], "\n"), "\n \t")
			if front != "" {
				body = front + "\n\n" + body
			}
		}
		sections = append(sections, section{title: titles[i], body: body})
	}
	return sections
}

var invalidFilenameChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

// sanitizeFilename turns heading text into a safe filename stem.
func sanitizeFilename(title string) string {
	name := invalidFilenameChars.ReplaceAllString(title, " ")
	name = strings.Join(strings.Fields(name), " ")
	name = strings.Trim(name, " .")
	if name == "" {
		name = "untitled"
	}
	const maxLen = 150
	if len(name) > maxLen {
		name = strings.TrimSpace(name[:maxLen])
	}
	return name
}

// uniqueFilename appends " (n)" on repeat headings so sections never
// overwrite each other.
func uniqueFilename(base string, counts map[string]int) string {
	counts[base]++
	if n := counts[base]; n > 1 {
		return fmt.Sprintf("%s (%d).md", base, n)
	}
	return base + ".md"
}
