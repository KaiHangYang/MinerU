package markdown

import (
	"os"
	"path/filepath"
	"testing"
)

const samplePaper = `# Some Paper Title

Authors here.

## Abstract

This is the abstract.

## 1 Introduction

Intro text.

## 2 Related Work

Related work text.
`

func TestSplitFileLevelZeroIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte(samplePaper), 0o644); err != nil {
		t.Fatal(err)
	}

	written, ok, err := SplitFile(path, 0)
	if err != nil {
		t.Fatalf("SplitFile: %v", err)
	}
	if ok || written != nil {
		t.Fatalf("expected no-op for level 0, got ok=%v written=%v", ok, written)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("original file should be untouched: %v", err)
	}
}

func TestSplitFileLevelOneHasSingleSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte(samplePaper), 0o644); err != nil {
		t.Fatal(err)
	}

	// Only one "#" heading exists (the title), so there's nothing to split.
	written, ok, err := SplitFile(path, 1)
	if err != nil {
		t.Fatalf("SplitFile: %v", err)
	}
	if ok || written != nil {
		t.Fatalf("expected no split with a single H1, got ok=%v written=%v", ok, written)
	}
}

func TestSplitFileLevelTwo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(path, []byte(samplePaper), 0o644); err != nil {
		t.Fatal(err)
	}

	written, ok, err := SplitFile(path, 2)
	if err != nil {
		t.Fatalf("SplitFile: %v", err)
	}
	if !ok {
		t.Fatalf("expected a split to happen")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("original file should have been removed, stat err = %v", err)
	}

	wantNames := []string{"Abstract.md", "1 Introduction.md", "2 Related Work.md"}
	if len(written) != len(wantNames) {
		t.Fatalf("got %d files, want %d: %v", len(written), len(wantNames), written)
	}
	for i, want := range wantNames {
		if got := filepath.Base(written[i]); got != want {
			t.Errorf("file %d = %q, want %q", i, got, want)
		}
	}

	abstract, err := os.ReadFile(filepath.Join(dir, "Abstract.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(abstract)
	if want := "# Some Paper Title\n\nAuthors here.\n\n## Abstract\n\nThis is the abstract.\n"; got != want {
		t.Fatalf("Abstract.md content = %q, want %q", got, want)
	}

	intro, err := os.ReadFile(filepath.Join(dir, "1 Introduction.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "## 1 Introduction\n\nIntro text.\n"; string(intro) != want {
		t.Fatalf("1 Introduction.md content = %q, want %q", intro, want)
	}
}

func TestSplitFileDuplicateHeadings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	content := "## Overview\n\nfirst\n\n## Overview\n\nsecond\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	written, ok, err := SplitFile(path, 2)
	if err != nil {
		t.Fatalf("SplitFile: %v", err)
	}
	if !ok {
		t.Fatalf("expected a split to happen")
	}
	wantNames := []string{"Overview.md", "Overview (2).md"}
	for i, want := range wantNames {
		if got := filepath.Base(written[i]); got != want {
			t.Errorf("file %d = %q, want %q", i, got, want)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"3.2 Anchor-Guided Persistent Memory": "3.2 Anchor-Guided Persistent Memory",
		"A/B: what?":                          "A B what",
		"  spaced  out  ":                     "spaced out",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
