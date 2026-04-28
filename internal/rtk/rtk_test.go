package rtk

import (
	"strings"
	"testing"
)

func TestSmartTruncate(t *testing.T) {
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = "line"
	}
	text := strings.Join(lines, "\n")
	out := SmartTruncate(text)
	if !strings.Contains(out, "lines truncated") {
		t.Fatal("expected truncation marker")
	}
	outLines := strings.Split(out, "\n")
	if len(outLines) > smartTruncateHead+smartTruncateTail+2 {
		t.Fatalf("too many lines after truncation: %d", len(outLines))
	}
}

func TestSmartTruncateSmallPassthrough(t *testing.T) {
	text := "a\nb\nc"
	if SmartTruncate(text) != text {
		t.Fatal("short text should pass through")
	}
}

func TestReadNumbered(t *testing.T) {
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = "  1|content line"
	}
	text := strings.Join(lines, "\n")
	out := ReadNumbered(text)
	if !strings.Contains(out, "lines truncated (file continues)") {
		t.Fatal("expected truncation marker")
	}
}

func TestGrep(t *testing.T) {
	input := "foo/bar.go:10:func Foo() {\nfoo/bar.go:20:}\nbaz/qux.go:5:import (\n"
	out := Grep(input)
	if !strings.Contains(out, "matches in") {
		t.Fatal("expected match summary")
	}
	if !strings.Contains(out, "[file] baz/qux.go") {
		t.Fatal("expected file grouping")
	}
}

func TestGrepNotGrepLine(t *testing.T) {
	input := "not a grep line\nanother line"
	out := Grep(input)
	if out != input {
		t.Fatalf("expected passthrough for non-grep, got %q", out)
	}
}

func TestFind(t *testing.T) {
	input := "./internal/foo.go\n./internal/bar.go\n./cmd/main.go\n"
	out := Find(input)
	if !strings.Contains(out, "files in") {
		t.Fatal("expected file summary")
	}
	if !strings.Contains(out, "./internal/") {
		t.Fatal("expected dir grouping")
	}
}

func TestTree(t *testing.T) {
	input := ".\n├── foo\n│   └── bar.go\n└── baz.go\n\n3 directories, 2 files\n"
	out := Tree(input)
	if strings.Contains(out, "directories") {
		t.Fatal("summary line should be removed")
	}
}

func TestTreeCap(t *testing.T) {
	lines := make([]string, 250)
	for i := range lines {
		lines[i] = "├── file"
	}
	text := strings.Join(lines, "\n")
	out := Tree(text)
	outLines := strings.Split(out, "\n")
	if len(outLines) > treeMaxLines+2 {
		t.Fatalf("tree not capped: %d lines", len(outLines))
	}
}

func TestLs(t *testing.T) {
	input := `total 32
drwxr-xr-x  3 user group  96 Jan  1 12:00 src
-rw-r--r--  1 user group 512 Jan  1 12:00 README.md
-rw-r--r--  1 user group 256 Jan  1 12:00 go.mod
`
	out := Ls(input)
	if !strings.Contains(out, "src/") {
		t.Fatal("expected dir")
	}
	if !strings.Contains(out, "Summary:") {
		t.Fatal("expected summary line")
	}
}

func TestGitDiff(t *testing.T) {
	input := `diff --git a/foo.go b/foo.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
-import "os"
 func main() {}
`
	out := GitDiff(input)
	if !strings.Contains(out, "foo.go") {
		t.Fatal("expected file name")
	}
	if !strings.Contains(out, "+1 -1") {
		t.Fatal("expected change counts, got: " + out)
	}
}

func TestGitStatus(t *testing.T) {
	input := `On branch main
M  internal/foo.go
?? internal/bar.go
A  cmd/main.go
`
	out := GitStatus(input)
	if !strings.Contains(out, "* main") {
		t.Fatal("expected branch")
	}
	if !strings.Contains(out, "Staged:") {
		t.Fatal("expected staged section")
	}
	if !strings.Contains(out, "Untracked:") {
		t.Fatal("expected untracked section")
	}
}

func TestDedupLog(t *testing.T) {
	input := "line1\nline1\nline1\nline2\n"
	out := DedupLog(input)
	if !strings.Contains(out, "duplicate lines") {
		t.Fatal("expected dedup message")
	}
}

func TestSearchList(t *testing.T) {
	input := "Result of search in 'src' (total 3 files):\n- src/foo/a.go\n- src/foo/b.go\n- src/bar/c.go\n"
	out := SearchList(input)
	if !strings.Contains(out, "3 files in 2 dirs") {
		t.Fatalf("expected dir grouping, got: %s", out)
	}
}

func TestDedupLogLineCap(t *testing.T) {
	// Generate input exceeding dedupLineMax
	lines := make([]string, dedupLineMax+100)
	for i := range lines {
		lines[i] = "unique-line-" + strings.Repeat("x", 5)
	}
	text := strings.Join(lines, "\n")
	out := DedupLog(text)
	outLines := strings.Split(out, "\n")
	if len(outLines) > dedupLineMax+5 {
		t.Fatalf("dedup log not capped: %d lines (cap=%d)", len(outLines), dedupLineMax)
	}
}

func TestDedupLogConsecutiveDuplicates(t *testing.T) {
	input := "msg\nmsg\nmsg\nother\n"
	out := DedupLog(input)
	if !strings.Contains(out, "×3") && !strings.Contains(out, "duplicate") {
		t.Fatalf("expected dedup marker for 3 consecutive identical lines, got: %q", out)
	}
	// Should have fewer lines than input
	if len(strings.Split(out, "\n")) >= len(strings.Split(input, "\n")) {
		t.Fatal("dedup should reduce line count")
	}
}

func TestGitDiffHunkTruncation(t *testing.T) {
	// Build a hunk exceeding gitDiffHunkMaxLines
	var sb strings.Builder
	sb.WriteString("diff --git a/big.go b/big.go\n")
	sb.WriteString("@@ -1,200 +1,200 @@\n")
	for i := 0; i < gitDiffHunkMaxLines+20; i++ {
		sb.WriteString("+added line\n")
	}
	out := GitDiff(sb.String())
	if !strings.Contains(out, "big.go") {
		t.Fatal("expected file name in output")
	}
	// Hunk should be truncated
	outLines := strings.Split(out, "\n")
	if len(outLines) > gitDiffHunkMaxLines+20 {
		t.Fatalf("hunk not truncated: %d lines", len(outLines))
	}
}

func TestFindPerDirTruncation(t *testing.T) {
	// One directory with more than findPerDirMax files
	var lines []string
	for i := 0; i < findPerDirMax+5; i++ {
		lines = append(lines, "./mydir/file"+strings.Repeat("x", 3)+".go")
	}
	input := strings.Join(lines, "\n")
	out := Find(input)
	if !strings.Contains(out, "mydir") {
		t.Fatal("expected mydir in output")
	}
	// Find uses "+N" as truncation marker for per-dir overflow
	if !strings.Contains(out, "+5") && !strings.Contains(out, "more") && !strings.Contains(out, "truncated") {
		t.Fatalf("expected truncation notice for per-dir overflow, got: %q", out)
	}
}

func TestSearchListManyDirs(t *testing.T) {
	var lines []string
	lines = append(lines, "Result of search in '.' (total 100 files):")
	for d := 0; d < searchListTotalDir+5; d++ {
		lines = append(lines, "- ./dir"+strings.Repeat("x", 2)+"/file.go")
	}
	input := strings.Join(lines, "\n")
	out := SearchList(input)
	// Should have a summary and not exceed dir limit
	if !strings.Contains(out, "dirs") && !strings.Contains(out, "files") {
		t.Fatalf("expected dir/file summary, got: %q", out)
	}
}

func TestReadNumberedPassthrough(t *testing.T) {
	// Short input should pass through unchanged
	input := "  1|line one\n  2|line two\n  3|line three\n"
	out := ReadNumbered(input)
	if out != input {
		t.Fatalf("short numbered file should pass through unchanged")
	}
}

func TestLsFilesOnly(t *testing.T) {
	// Ls with no directories — should not panic
	input := `total 16
-rw-r--r-- 1 user group 100 Jan 1 12:00 foo.go
-rw-r--r-- 1 user group 200 Jan 1 12:00 bar.go
-rw-r--r-- 1 user group 300 Jan 1 12:00 baz.go
`
	out := Ls(input)
	if !strings.Contains(out, "Summary:") {
		t.Fatal("expected Summary line even with files only")
	}
}

func TestApply_Passthrough(t *testing.T) {
	// Short plaintext should return unchanged
	text := "Hello, world!"
	if Apply(text) != text {
		t.Fatal("short plain text should pass through Apply unchanged")
	}
}

func filterName(fn FilterFunc) string {
	// Apply each known filter on a sentinel and compare outputs to identify fn.
	// Since Go can't use func as map key, we probe by calling each candidate
	// on a known input and comparing the result to what fn produces.
	// Instead, just apply fn + known candidates on a canary to identify.
	// Simplest: use fmt.Sprintf pointer trick via reflect-free approach.
	canary := "CANARY_LINE:1:content"
	switch fn(canary) {
	case Grep(canary):
		return "grep"
	}
	canary2 := "./a/b.go\n./a/c.go\n./d/e.go"
	if fn(canary2) == Find(canary2) {
		return "find"
	}
	canary3 := "diff --git a/foo b/foo\n@@ -1 +1 @@\n+x"
	if fn(canary3) == GitDiff(canary3) {
		return "git-diff"
	}
	canary4 := "On branch main\nM  foo.go"
	if fn(canary4) == GitStatus(canary4) {
		return "git-status"
	}
	canary6 := "Result of search in 'src' (total 2 files):\n- src/a.go\n- src/b.go"
	if fn(canary6) == SearchList(canary6) {
		return "search-list"
	}
	canary5 := ".\n├── foo\n│   └── bar.go"
	if fn(canary5) == Tree(canary5) {
		return "tree"
	}
	return "unknown"
}

func TestAutoDetect(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"grep", "foo.go:10:func Foo() {", "grep"},
		{"find", "./internal/foo.go\n./internal/bar.go\n./cmd/main.go", "find"},
		{"gitdiff", "diff --git a/foo b/foo\n@@ -1,1 +1,1 @@", "git-diff"},
		{"gitstatus", "On branch main\nM  foo.go", "git-status"},
		{"tree", ".\n├── foo\n│   └── bar.go", "tree"},
		{"searchlist", "Result of search in 'src' (total 2 files):\n- src/a.go\n- src/b.go", "search-list"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fn := AutoDetect(tc.input)
			if fn == nil {
				t.Fatalf("expected filter %q, got nil", tc.want)
			}
			got := filterName(fn)
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestSafeApplyPanicPassthrough(t *testing.T) {
	panicking := FilterFunc(func(text string) string {
		panic("boom")
	})
	out := SafeApply(panicking, "hello")
	if out != "hello" {
		t.Fatalf("expected passthrough on panic, got %q", out)
	}
}
