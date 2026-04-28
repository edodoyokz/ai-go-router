// Package rtk provides response-text-kompression filters that compact common
// tool-output formats (grep, find, git diff, git status, ls, tree, etc.) before
// they are sent to the LLM, mirroring the reference open-sse/rtk behaviour.
package rtk

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// --- Constants (mirror reference rtk/constants.js) ---

const (
	detectWindow         = 1024
	grepPerFileMax       = 10
	findPerDirMax        = 10
	findTotalDirMax      = 20
	statusMaxFiles       = 10
	statusMaxUntracked   = 10
	lsExtSummaryTop      = 5
	treeMaxLines         = 200
	searchListPerDirMax  = 10
	searchListTotalDir   = 20
	smartTruncateHead    = 120
	smartTruncateTail    = 60
	smartTruncateMinLines = 250
	readNumberedMinHitRatio = 0.7
	dedupLineMax         = 2000
	gitDiffHunkMaxLines  = 100
)

var lsNoiseDirs = map[string]bool{
	"node_modules": true, ".git": true, "target": true, "__pycache__": true,
	".next": true, "dist": true, "build": true, ".venv": true, "venv": true,
	".cache": true, ".idea": true, ".vscode": true, ".DS_Store": true,
}

// FilterFunc applies a filter to text. On panic/error it passes through unchanged.
type FilterFunc func(text string) string

// SafeApply runs fn(text) and returns the original text on any panic.
func SafeApply(fn FilterFunc, text string) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = text
		}
	}()
	if fn == nil {
		return text
	}
	out := fn(text)
	return out
}

// AutoDetect returns the best filter for text, or nil for passthrough.
func AutoDetect(text string) FilterFunc {
	head := text
	if len(head) > detectWindow {
		head = head[:detectWindow]
	}

	reGitDiff := regexp.MustCompile(`(?m)^diff --git `)
	reGitDiffHunk := regexp.MustCompile(`(?m)^@@ `)
	reGitStatus := regexp.MustCompile(`(?m)(^On branch |^nothing to commit|^Changes (not |to be )|^Untracked files:)`)
	rePorcelain := regexp.MustCompile(`(?m)^[ MADRCU?!][ MADRCU?!] \S`)
	reTreeGlyph := regexp.MustCompile(`[├└]──|│  `)
	reLsRow := regexp.MustCompile(`(?m)^[-dlbcps][rwx-]{9}`)
	reLsTotal := regexp.MustCompile(`(?m)^total \d+$`)

	if reGitDiff.MatchString(head) || reGitDiffHunk.MatchString(head) {
		return GitDiff
	}
	if reGitStatus.MatchString(head) || isMostlyPorcelain(head, rePorcelain) {
		return GitStatus
	}

	lines := strings.Split(head, "\n")
	nonEmpty := filterNonEmpty(lines)

	first5 := nonEmpty
	if len(first5) > 5 {
		first5 = first5[:5]
	}
	if anyGrepLine(first5) {
		return Grep
	}

	if len(nonEmpty) >= 3 && allPathLike(nonEmpty) {
		return Find
	}

	if reTreeGlyph.MatchString(head) {
		return Tree
	}

	if reLsTotal.MatchString(head) || countRegexpMatches(head, reLsRow) >= 3 {
		return Ls
	}

	if searchListHeaderRE.MatchString(head) {
		return SearchList
	}

	allLines := strings.Split(text, "\n")
	if len(allLines) >= smartTruncateMinLines && isLineNumbered(allLines) {
		return ReadNumbered
	}

	if len(nonEmpty) >= 5 {
		return DedupLog
	}

	if len(strings.Split(text, "\n")) >= smartTruncateMinLines {
		return SmartTruncate
	}

	return nil
}

// Apply autodetects and applies the appropriate filter.
func Apply(text string) string {
	fn := AutoDetect(text)
	if fn == nil {
		return text
	}
	return SafeApply(fn, text)
}

// --- Filter implementations ---

// SmartTruncate keeps head+tail lines and replaces the middle with a count.
func SmartTruncate(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) < smartTruncateMinLines {
		return text
	}
	head := lines[:smartTruncateHead]
	tail := lines[len(lines)-smartTruncateTail:]
	cut := len(lines) - len(head) - len(tail)
	var sb strings.Builder
	sb.WriteString(strings.Join(head, "\n"))
	sb.WriteString(fmt.Sprintf("\n... +%d lines truncated\n", cut))
	sb.WriteString(strings.Join(tail, "\n"))
	return sb.String()
}

// ReadNumbered truncates numbered-file-dump output ("  N|content").
func ReadNumbered(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) < smartTruncateMinLines {
		return text
	}
	head := lines[:smartTruncateHead]
	tail := lines[len(lines)-smartTruncateTail:]
	cut := len(lines) - len(head) - len(tail)
	var sb strings.Builder
	sb.WriteString(strings.Join(head, "\n"))
	sb.WriteString(fmt.Sprintf("\n... +%d lines truncated (file continues)\n", cut))
	sb.WriteString(strings.Join(tail, "\n"))
	return sb.String()
}

var readNumberedLineRE = regexp.MustCompile(`^\s*\d+\|`)

// Grep compacts grep output (file:lineno:content) grouping matches by file.
func Grep(text string) string {
	type match struct {
		lineNo  string
		content string
	}
	byFile := map[string][]match{}
	var fileOrder []string
	total := 0

	for _, line := range strings.Split(text, "\n") {
		first := strings.Index(line, ":")
		if first == -1 {
			continue
		}
		rest := line[first+1:]
		second := strings.Index(rest, ":")
		if second == -1 {
			continue
		}
		file := line[:first]
		lineNoStr := rest[:second]
		content := rest[second+1:]
		if !isDigits(lineNoStr) {
			continue
		}
		total++
		if _, exists := byFile[file]; !exists {
			fileOrder = append(fileOrder, file)
		}
		byFile[file] = append(byFile[file], match{lineNoStr, content})
	}

	if total == 0 {
		return text
	}

	sort.Strings(fileOrder)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d matches in %dF:\n\n", total, len(fileOrder)))
	for _, file := range fileOrder {
		matches := byFile[file]
		sb.WriteString(fmt.Sprintf("[file] %s (%d):\n", file, len(matches)))
		show := matches
		if len(show) > grepPerFileMax {
			show = show[:grepPerFileMax]
		}
		for _, m := range show {
			sb.WriteString(fmt.Sprintf("  %4s: %s\n", m.lineNo, strings.TrimSpace(m.content)))
		}
		if len(matches) > grepPerFileMax {
			sb.WriteString(fmt.Sprintf("  +%d\n", len(matches)-grepPerFileMax))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// Find compacts find-style path output by grouping basenames under parent dirs.
func Find(text string) string {
	lines := filterNonEmpty(strings.Split(text, "\n"))
	if len(lines) == 0 {
		return text
	}

	byDir := map[string][]string{}
	var dirOrder []string
	for _, path := range lines {
		idx := strings.LastIndex(path, "/")
		var dir, base string
		if idx == -1 {
			dir = "."
			base = path
		} else {
			dir = path[:idx]
			if dir == "" {
				dir = "/"
			}
			base = path[idx+1:]
		}
		if _, exists := byDir[dir]; !exists {
			dirOrder = append(dirOrder, dir)
		}
		byDir[dir] = append(byDir[dir], base)
	}

	sort.Strings(dirOrder)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d files in %d dirs:\n\n", len(lines), len(dirOrder)))
	showDirs := dirOrder
	if len(showDirs) > findTotalDirMax {
		showDirs = showDirs[:findTotalDirMax]
	}
	for _, dir := range showDirs {
		files := byDir[dir]
		sb.WriteString(fmt.Sprintf("%s/ (%d):\n", dir, len(files)))
		show := files
		if len(show) > findPerDirMax {
			show = show[:findPerDirMax]
		}
		for _, f := range show {
			sb.WriteString("  " + f + "\n")
		}
		if len(files) > findPerDirMax {
			sb.WriteString(fmt.Sprintf("  +%d\n", len(files)-findPerDirMax))
		}
		sb.WriteString("\n")
	}
	if len(dirOrder) > findTotalDirMax {
		sb.WriteString(fmt.Sprintf("+%d more dirs\n", len(dirOrder)-findTotalDirMax))
	}
	return sb.String()
}

// Tree removes the summary line and trailing blanks from tree output, caps at 200 lines.
func Tree(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text
	}
	var filtered []string
	for _, line := range lines {
		if strings.Contains(line, "director") && strings.Contains(line, "file") {
			continue
		}
		if strings.TrimSpace(line) == "" && len(filtered) == 0 {
			continue
		}
		filtered = append(filtered, line)
	}
	for len(filtered) > 0 && strings.TrimSpace(filtered[len(filtered)-1]) == "" {
		filtered = filtered[:len(filtered)-1]
	}
	if len(filtered) > treeMaxLines {
		cut := len(filtered) - treeMaxLines
		return strings.Join(filtered[:treeMaxLines], "\n") + fmt.Sprintf("\n... +%d more lines", cut)
	}
	return strings.Join(filtered, "\n")
}

// Ls compacts ls -la output to a compact name/size listing with extension summary.
func Ls(text string) string {
	lsDateRE := regexp.MustCompile(`\s+(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}\s+(\d{4}|\d{2}:\d{2})\s+`)

	type entry struct {
		name     string
		size     int64
		fileType byte
	}
	var dirs []string
	var files []entry
	byExt := map[string]int{}

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "total ") || line == "" {
			continue
		}
		m := lsDateRE.FindStringIndex(line)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(line[m[1]:])
		before := line[:m[0]]
		parts := strings.Fields(before)
		if len(parts) < 4 || len(parts[0]) < 1 {
			continue
		}
		if name == "." || name == ".." {
			continue
		}
		ft := parts[0][0]
		var size int64
		for i := len(parts) - 1; i >= 0; i-- {
			n, err := strconv.ParseInt(parts[i], 10, 64)
			if err == nil {
				size = n
				break
			}
		}
		switch ft {
		case 'd':
			if !lsNoiseDirs[name] {
				dirs = append(dirs, name)
			}
		case '-', 'l':
			if !lsNoiseDirs[name] {
				dot := strings.LastIndex(name, ".")
				ext := "no ext"
				if dot > 0 {
					ext = name[dot:]
				}
				byExt[ext]++
				files = append(files, entry{name, size, ft})
			}
		}
	}

	if len(dirs) == 0 && len(files) == 0 {
		return text
	}

	var sb strings.Builder
	for _, d := range dirs {
		sb.WriteString(d + "/\n")
	}
	for _, f := range files {
		sb.WriteString(f.name + "  " + humanSize(f.size) + "\n")
	}

	type extCount struct {
		ext string
		cnt int
	}
	var extList []extCount
	for e, c := range byExt {
		extList = append(extList, extCount{e, c})
	}
	sort.Slice(extList, func(i, j int) bool { return extList[i].cnt > extList[j].cnt })

	sb.WriteString(fmt.Sprintf("\nSummary: %d files, %d dirs", len(files), len(dirs)))
	if len(extList) > 0 {
		show := extList
		if len(show) > lsExtSummaryTop {
			show = show[:lsExtSummaryTop]
		}
		var parts []string
		for _, ec := range show {
			parts = append(parts, fmt.Sprintf("%d %s", ec.cnt, ec.ext))
		}
		sb.WriteString(" (")
		sb.WriteString(strings.Join(parts, ", "))
		if len(extList) > lsExtSummaryTop {
			sb.WriteString(fmt.Sprintf(", +%d more", len(extList)-lsExtSummaryTop))
		}
		sb.WriteString(")")
	}
	return sb.String()
}

// GitDiff compacts unified diff output with per-hunk line caps.
func GitDiff(text string) string {
	return gitDiffInternal(text, 500)
}

func gitDiffInternal(diff string, maxLines int) string {
	var result []string
	currentFile := ""
	added := 0
	removed := 0
	inHunk := false
	hunkShown := 0
	hunkSkipped := 0
	wasTruncated := false

	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			if hunkSkipped > 0 {
				result = append(result, fmt.Sprintf("  ... (%d lines truncated)", hunkSkipped))
				wasTruncated = true
				hunkSkipped = 0
			}
			if currentFile != "" && (added > 0 || removed > 0) {
				result = append(result, fmt.Sprintf("  +%d -%d", added, removed))
			}
			parts := strings.SplitN(line, " b/", 2)
			if len(parts) > 1 {
				currentFile = parts[1]
			} else {
				currentFile = "unknown"
			}
			result = append(result, "\n"+currentFile)
			added, removed = 0, 0
			inHunk = false
			hunkShown = 0
		} else if strings.HasPrefix(line, "@@") {
			if hunkSkipped > 0 {
				result = append(result, fmt.Sprintf("  ... (%d lines truncated)", hunkSkipped))
				wasTruncated = true
				hunkSkipped = 0
			}
			inHunk = true
			hunkShown = 0
			result = append(result, "  "+line)
		} else if inHunk {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				added++
				if hunkShown < gitDiffHunkMaxLines {
					result = append(result, "  "+line)
					hunkShown++
				} else {
					hunkSkipped++
				}
			} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				removed++
				if hunkShown < gitDiffHunkMaxLines {
					result = append(result, "  "+line)
					hunkShown++
				} else {
					hunkSkipped++
				}
			} else if hunkShown < gitDiffHunkMaxLines && !strings.HasPrefix(line, "\\") {
				if hunkShown > 0 {
					result = append(result, "  "+line)
					hunkShown++
				}
			}
		}

		if len(result) >= maxLines {
			result = append(result, "\n... (more changes truncated)")
			wasTruncated = true
			break
		}
	}

	if hunkSkipped > 0 {
		result = append(result, fmt.Sprintf("  ... (%d lines truncated)", hunkSkipped))
		wasTruncated = true
	}
	if currentFile != "" && (added > 0 || removed > 0) {
		result = append(result, fmt.Sprintf("  +%d -%d", added, removed))
	}
	if wasTruncated {
		result = append(result, "[full diff: rtk git diff --no-compact]")
	}
	return strings.Join(result, "\n")
}

// GitStatus compacts git status output.
func GitStatus(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || (len(lines) == 1 && strings.TrimSpace(lines[0]) == "") {
		return "Clean working tree"
	}

	branch := ""
	var stagedFiles, modifiedFiles, untrackedFiles []string
	staged, modified, untracked, conflicts := 0, 0, 0, 0

	reLongBranch := regexp.MustCompile(`^On branch (\S+)`)
	reLongMatch := regexp.MustCompile(`^\s*(modified|new file|deleted|renamed|both modified):\s+(.+)$`)

	for _, raw := range lines {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if m := reLongBranch.FindStringSubmatch(raw); m != nil {
			branch = m[1]
			continue
		}
		if strings.HasPrefix(raw, "##") {
			branch = strings.TrimPrefix(raw, "## ")
			continue
		}
		if len(raw) >= 3 && isPorcelainLine(raw) {
			x, y := raw[0], raw[1]
			file := raw[3:]
			if raw[:2] == "??" {
				untracked++
				untrackedFiles = append(untrackedFiles, file)
				continue
			}
			if strings.ContainsRune("MADRC", rune(x)) {
				staged++
				stagedFiles = append(stagedFiles, file)
			} else if x == 'U' {
				conflicts++
			}
			if y == 'M' || y == 'D' {
				modified++
				modifiedFiles = append(modifiedFiles, file)
			}
			continue
		}
		if m := reLongMatch.FindStringSubmatch(raw); m != nil {
			kind, path := m[1], strings.TrimSpace(m[2])
			switch kind {
			case "both modified":
				conflicts++
			case "modified", "deleted":
				modified++
				modifiedFiles = append(modifiedFiles, path)
			case "new file", "renamed":
				staged++
				stagedFiles = append(stagedFiles, path)
			}
		}
	}

	var sb strings.Builder
	if branch != "" {
		sb.WriteString("* " + branch + "\n")
	}
	if staged > 0 {
		sb.WriteString(fmt.Sprintf("+ Staged: %d files\n", staged))
		for _, f := range limitSlice(stagedFiles, statusMaxFiles) {
			sb.WriteString("   " + f + "\n")
		}
		if len(stagedFiles) > statusMaxFiles {
			sb.WriteString(fmt.Sprintf("   ... +%d more\n", len(stagedFiles)-statusMaxFiles))
		}
	}
	if modified > 0 {
		sb.WriteString(fmt.Sprintf("~ Modified: %d files\n", modified))
		for _, f := range limitSlice(modifiedFiles, statusMaxFiles) {
			sb.WriteString("   " + f + "\n")
		}
		if len(modifiedFiles) > statusMaxFiles {
			sb.WriteString(fmt.Sprintf("   ... +%d more\n", len(modifiedFiles)-statusMaxFiles))
		}
	}
	if untracked > 0 {
		sb.WriteString(fmt.Sprintf("? Untracked: %d files\n", untracked))
		for _, f := range limitSlice(untrackedFiles, statusMaxUntracked) {
			sb.WriteString("   " + f + "\n")
		}
		if len(untrackedFiles) > statusMaxUntracked {
			sb.WriteString(fmt.Sprintf("   ... +%d more\n", len(untrackedFiles)-statusMaxUntracked))
		}
	}
	if conflicts > 0 {
		sb.WriteString(fmt.Sprintf("conflicts: %d files\n", conflicts))
	}
	if staged == 0 && modified == 0 && untracked == 0 && conflicts == 0 {
		sb.WriteString("clean — nothing to commit\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// DedupLog collapses consecutive duplicate lines and caps at 2000.
func DedupLog(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	prev := ""
	runCount := 0
	blankStreak := 0
	hasPrev := false

	flushRun := func() {
		if hasPrev && runCount > 1 {
			out = append(out, fmt.Sprintf("  ... (%d duplicate lines)", runCount-1))
		}
	}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if blankStreak < 1 {
				out = append(out, line)
			}
			blankStreak++
			flushRun()
			prev = ""
			runCount = 0
			hasPrev = false
			continue
		}
		blankStreak = 0
		if hasPrev && line == prev {
			runCount++
			continue
		}
		flushRun()
		out = append(out, line)
		prev = line
		runCount = 1
		hasPrev = true
		if len(out) >= dedupLineMax {
			out = append(out, fmt.Sprintf("... (truncated at %d lines)", dedupLineMax))
			return strings.Join(out, "\n")
		}
	}
	flushRun()
	return strings.Join(out, "\n")
}

var searchListHeaderRE = regexp.MustCompile(`^Result of search in '[^']*' \(total \d+ files?\):`)

// SearchList compacts Cursor Glob search output.
func SearchList(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text
	}
	header := lines[0]
	rest := lines[1:]

	var paths []string
	for _, raw := range rest {
		t := strings.TrimSpace(raw)
		if !strings.HasPrefix(t, "- ") {
			continue
		}
		paths = append(paths, t[2:])
	}
	if len(paths) == 0 {
		return text
	}

	byDir := map[string][]string{}
	var dirOrder []string
	for _, p := range paths {
		slash := strings.LastIndex(p, "/")
		var dir, name string
		if slash == -1 {
			dir = "."
			name = p
		} else {
			dir = p[:slash]
			if dir == "" {
				dir = "/"
			}
			name = p[slash+1:]
		}
		if _, exists := byDir[dir]; !exists {
			dirOrder = append(dirOrder, dir)
		}
		byDir[dir] = append(byDir[dir], name)
	}
	sort.Strings(dirOrder)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n%d files in %d dirs:\n\n", header, len(paths), len(dirOrder)))
	showDirs := dirOrder
	if len(showDirs) > searchListTotalDir {
		showDirs = showDirs[:searchListTotalDir]
	}
	for _, dir := range showDirs {
		names := byDir[dir]
		sb.WriteString(fmt.Sprintf("%s/ (%d):\n", dir, len(names)))
		show := names
		if len(show) > searchListPerDirMax {
			show = show[:searchListPerDirMax]
		}
		for _, n := range show {
			sb.WriteString("  " + n + "\n")
		}
		if len(names) > searchListPerDirMax {
			sb.WriteString(fmt.Sprintf("  +%d\n", len(names)-searchListPerDirMax))
		}
		sb.WriteString("\n")
	}
	if len(dirOrder) > searchListTotalDir {
		sb.WriteString(fmt.Sprintf("+%d more dirs\n", len(dirOrder)-searchListTotalDir))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// --- helpers ---

func humanSize(bytes int64) string {
	switch {
	case bytes >= 1048576:
		return fmt.Sprintf("%.1fM", float64(bytes)/1048576)
	case bytes >= 1024:
		return fmt.Sprintf("%.1fK", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func filterNonEmpty(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func anyGrepLine(lines []string) bool {
	for _, l := range lines {
		first := strings.Index(l, ":")
		if first == -1 {
			continue
		}
		rest := l[first+1:]
		second := strings.Index(rest, ":")
		if second == -1 {
			continue
		}
		if isDigits(rest[:second]) {
			return true
		}
	}
	return false
}

func allPathLike(lines []string) bool {
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if strings.Contains(t, ":") {
			return false
		}
		if !strings.HasPrefix(t, ".") && !strings.HasPrefix(t, "/") && !strings.Contains(t, "/") {
			return false
		}
	}
	return true
}

func isMostlyPorcelain(head string, re *regexp.Regexp) bool {
	lines := filterNonEmpty(strings.Split(head, "\n"))
	if len(lines) < 3 {
		return false
	}
	hits := 0
	for _, l := range lines {
		if re.MatchString(l) {
			hits++
		}
	}
	return float64(hits)/float64(len(lines)) >= 0.6
}

func isPorcelainLine(raw string) bool {
	if len(raw) < 3 {
		return false
	}
	valid := " MADRCU?!"
	return strings.ContainsRune(valid, rune(raw[0])) && strings.ContainsRune(valid, rune(raw[1])) && raw[2] == ' '
}

func isLineNumbered(lines []string) bool {
	sample := lines
	if len(sample) > 100 {
		sample = sample[:100]
	}
	hits, nonEmpty := 0, 0
	for _, l := range sample {
		if l == "" {
			continue
		}
		nonEmpty++
		if readNumberedLineRE.MatchString(l) {
			hits++
		}
	}
	if nonEmpty < 5 {
		return false
	}
	return float64(hits)/float64(nonEmpty) >= readNumberedMinHitRatio
}

func countRegexpMatches(text string, re *regexp.Regexp) int {
	return len(re.FindAllString(text, -1))
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func limitSlice(ss []string, n int) []string {
	if len(ss) <= n {
		return ss
	}
	return ss[:n]
}
