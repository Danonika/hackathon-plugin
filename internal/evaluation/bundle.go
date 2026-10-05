package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DefaultBundleExcludes keeps mutable runtime state, secrets and generated
// mirrors out of a judge bundle.
var DefaultBundleExcludes = []string{
	".git",
	".env",
	".env*",
	"bin",
	"logs",
	"evals",
	"data/artifacts",
	"data/index",
	"data/judge-bundles",
	"judge-bundle",
	"node_modules",
	".agents/skills",
	"plugins/hackathonkit/.mcp.json",
}

type BundleOptions struct {
	Root          string
	OutDir        string
	Exclude       []string
	ReadOnly      bool
	Model         string
	RubricVersion string
	// Profile selects what the bundle contains: "full" (default) keeps the
	// repository minus runtime state, "technical" is an alias for full, and
	// "demo" keeps only presentation evidence (regulation 9.4: Demo Day judges
	// do not re-check code or tests).
	Profile string
}

// demoIncludes are the only entries copied for the demo profile.
var demoIncludes = []string{
	"TASK.md",
	"README.md",
	"architecture.md",
	"known-limitations.md",
	"demo-artifacts",
	"demo",
	"demo-script.md",
	"presentation.md",
}

func normalizeProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "demo":
		return "demo"
	default:
		return "full"
	}
}

// bundleReserved are root-level names this tool owns and writes itself. They
// must never be copied from the repository, or a repository file of the same
// name would overwrite the marker/metadata and render the bundle unusable.
var bundleReserved = map[string]bool{
	".hackathonkit-bundle":   true,
	"git-sha.txt":        true,
	"rubric-version.txt": true,
	"bundle.json":        true,
	"bundle-hash.txt":    true,
}

func demoIncluded(rel string) bool {
	for _, include := range demoIncludes {
		if rel == include || strings.HasPrefix(rel, include+"/") {
			return true
		}
	}
	return false
}

type BundleFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type Bundle struct {
	Dir           string   `json:"dir"`
	BundleHash    string   `json:"bundle_hash"`
	GitSHA        string   `json:"git_sha"`
	Model         string   `json:"model,omitempty"`
	RubricVersion string   `json:"rubric_version"`
	Profile       string   `json:"profile"`
	Files         int      `json:"files"`
	Bytes         int64    `json:"bytes"`
	Excluded      []string `json:"excluded"`
}

// FreezeBundle copies a read-only snapshot of the repository (defaults to
// <root>/data/judge-bundles), writes git-sha.txt, rubric-version.txt, a bundle.json
// manifest and a deterministic bundle hash. The hash covers every copied file
// except bundle.json and bundle-hash.txt, so freezing the same tree twice
// yields the same hash.
func FreezeBundle(opts BundleOptions) (Bundle, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return Bundle{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return Bundle{}, fmt.Errorf("cannot stat bundle root %s: %w", root, err)
	}
	if !info.IsDir() {
		return Bundle{}, fmt.Errorf("bundle root is not a directory: %s", root)
	}
	outDir := opts.OutDir
	if strings.TrimSpace(outDir) == "" {
		outDir = filepath.Join(root, "data", "judge-bundles")
	}
	outAbs, err := filepath.Abs(outDir)
	if err != nil {
		return Bundle{}, err
	}
	rootResolved := resolveExistingPath(root)
	outResolved := resolveExistingPath(outAbs)
	relOut, err := filepath.Rel(rootResolved, outResolved)
	if err != nil || relOut == "." || relOut == ".." || strings.HasPrefix(relOut, ".."+string(filepath.Separator)) {
		return Bundle{}, fmt.Errorf("bundle output must be inside the repository root: %s", outDir)
	}
	outRel := filepath.ToSlash(relOut)

	excludes := append([]string{}, DefaultBundleExcludes...)
	for _, e := range opts.Exclude {
		e = strings.Trim(strings.ReplaceAll(strings.TrimSpace(e), "\\", "/"), "/")
		if e != "" {
			excludes = append(excludes, e)
		}
	}
	if err := validateExcludes(excludes); err != nil {
		return Bundle{}, err
	}
	excludeRules, err := compileExcludes(excludes)
	if err != nil {
		return Bundle{}, err
	}

	outEntries, readErr := os.ReadDir(outAbs)
	switch {
	case readErr == nil && len(outEntries) > 0:
		// Only delete a directory this tool owns (marker records the target).
		markerPath := filepath.Join(outAbs, ".hackathonkit-bundle")
		marker, markerErr := os.ReadFile(markerPath)
		if markerErr != nil || strings.TrimSpace(string(marker)) != outAbs {
			return Bundle{}, fmt.Errorf("refusing to delete %s: not a bundle created by this tool (missing ownership marker)", outDir)
		}
		if info, lerr := os.Lstat(outAbs); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			return Bundle{}, fmt.Errorf("refusing to delete %s: it is a symlink", outDir)
		}
	case readErr != nil && !os.IsNotExist(readErr):
		return Bundle{}, readErr
	}
	// Build the new bundle in a sibling temp directory and swap it into place
	// only once complete, so a failed run never destroys the previous bundle.
	if err := os.MkdirAll(filepath.Dir(outAbs), 0o755); err != nil {
		return Bundle{}, err
	}
	tmpAbs, err := os.MkdirTemp(filepath.Dir(outAbs), filepath.Base(outAbs)+".tmp-")
	if err != nil {
		return Bundle{}, err
	}
	// After a successful rename tmpAbs no longer exists, so this is a no-op.
	defer os.RemoveAll(tmpAbs)
	// Resolve symlinks before computing the walk-relative path: the walk is
	// rooted at rootResolved, so an unresolved sibling path could otherwise
	// fail to match and the temp dir would be copied into itself.
	tmpRel := outRel
	if rel, rerr := filepath.Rel(rootResolved, resolveExistingPath(tmpAbs)); rerr == nil {
		tmpRel = filepath.ToSlash(rel)
	}
	if err := os.WriteFile(filepath.Join(tmpAbs, ".hackathonkit-bundle"), []byte(outAbs+"\n"), 0o644); err != nil {
		return Bundle{}, err
	}

	gitSHA := resolveGitSHA(root)
	rubricVersion := strings.TrimSpace(opts.RubricVersion)
	if rubricVersion == "" {
		rubricVersion = RubricVersion
	}
	profile := normalizeProfile(opts.Profile)

	err = filepath.WalkDir(rootResolved, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(rootResolved, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash == "." {
			return nil
		}
		if isExcluded(relSlash, excludeRules) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			// Never copy the output directory, its temp siblings, or the
			// `.bak` rotation sibling. A stale `.bak` left by an interrupted
			// run is not covered by the default excludes (a path-prefix rule
			// does not match `…judge-bundles.bak`), so copy it only into the
			// new bundle would duplicate content and corrupt the hash.
			bakRel := outRel + ".bak"
			if relSlash == outRel || strings.HasPrefix(relSlash, outRel+"/") ||
				relSlash == tmpRel || strings.HasPrefix(relSlash, tmpRel+"/") ||
				relSlash == bakRel || strings.HasPrefix(relSlash, bakRel+"/") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if profile == "demo" && !demoIncluded(relSlash) {
			return nil
		}
		if !strings.Contains(relSlash, "/") && bundleReserved[relSlash] {
			return nil
		}
		dst := filepath.Join(tmpAbs, filepath.FromSlash(relSlash))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if _, err := copyFile(path, dst, opts.ReadOnly); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Bundle{}, err
	}

	if err := writeBundleMeta(tmpAbs, "git-sha.txt", gitSHA); err != nil {
		return Bundle{}, err
	}
	if err := writeBundleMeta(tmpAbs, "rubric-version.txt", rubricVersion); err != nil {
		return Bundle{}, err
	}

	entries, bundleHash, err := hashBundle(tmpAbs)
	if err != nil {
		return Bundle{}, err
	}
	var hashedBytes int64
	for _, e := range entries {
		hashedBytes += e.Bytes
	}
	manifest := map[string]any{
		"created_at":     time.Now().UTC().Format(time.RFC3339),
		"git_sha":        gitSHA,
		"model":          opts.Model,
		"rubric_version": rubricVersion,
		"profile":        profile,
		"bundle_hash":    bundleHash,
		"files":          entries,
		"excludes":       excludes,
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Bundle{}, err
	}
	manifestMode := os.FileMode(0o644)
	if opts.ReadOnly {
		manifestMode = 0o444
	}
	if err := os.WriteFile(filepath.Join(tmpAbs, "bundle.json"), append(manifestData, '\n'), manifestMode); err != nil {
		return Bundle{}, err
	}
	if err := os.WriteFile(filepath.Join(tmpAbs, "bundle-hash.txt"), []byte(bundleHash+"\n"), manifestMode); err != nil {
		return Bundle{}, err
	}
	// Swap the completed bundle into place without ever leaving the committed
	// bundle missing: verify ownership, move the live bundle aside, install the
	// new one, and only then delete the backup. If the install fails, roll the
	// backup back.
	if err := verifyOwnedBundle(outAbs, outDir); err != nil {
		return Bundle{}, err
	}
	backupAbs := outAbs + ".bak"
	if err := os.RemoveAll(backupAbs); err != nil {
		return Bundle{}, err
	}
	hadPrevious := false
	if _, err := os.Lstat(outAbs); err == nil {
		if err := os.Rename(outAbs, backupAbs); err != nil {
			return Bundle{}, err
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return Bundle{}, err
	}
	if err := os.Rename(tmpAbs, outAbs); err != nil {
		if hadPrevious {
			if rberr := os.Rename(backupAbs, outAbs); rberr != nil {
				return Bundle{}, fmt.Errorf("bundle install failed (%v) and rollback failed: %w", err, rberr)
			}
		}
		return Bundle{}, err
	}
	if hadPrevious {
		if err := os.RemoveAll(backupAbs); err != nil {
			return Bundle{}, err
		}
	}
	return Bundle{
		Dir:           outAbs,
		BundleHash:    bundleHash,
		GitSHA:        gitSHA,
		Model:         opts.Model,
		RubricVersion: rubricVersion,
		Profile:       profile,
		Files:         len(entries),
		Bytes:         hashedBytes,
		Excluded:      excludes,
	}, nil
}

// verifyOwnedBundle checks that a path this tool wants to replace is a bundle
// it created (ownership marker) and is not a symlink. A missing path is fine.
func verifyOwnedBundle(outAbs, outDir string) error {
	info, err := os.Lstat(outAbs)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace %s: it is a symlink", outDir)
	}
	if info.IsDir() {
		entries, rerr := os.ReadDir(outAbs)
		if rerr != nil {
			return rerr
		}
		if len(entries) > 0 {
			marker, merr := os.ReadFile(filepath.Join(outAbs, ".hackathonkit-bundle"))
			if merr != nil || strings.TrimSpace(string(marker)) != outAbs {
				return fmt.Errorf("refusing to replace %s: not a bundle created by this tool (missing ownership marker)", outDir)
			}
		}
	}
	return nil
}

type excludeRule struct {
	pattern  string
	hasSlash bool
	glob     bool
	re       *regexp.Regexp
}

// compileExcludes turns the exclude list into rules. A pattern with a slash
// matches a repository-relative path prefix, a pattern without a slash matches
// any file or directory base name, and patterns containing glob metacharacters
// are matched against both the full relative path and the base name. An invalid
// glob is reported rather than silently dropped (which would leak files that
// were meant to be excluded).
func compileExcludes(patterns []string) ([]excludeRule, error) {
	rules := make([]excludeRule, 0, len(patterns))
	seen := map[string]struct{}{}
	for _, pattern := range patterns {
		pattern = strings.Trim(strings.ReplaceAll(strings.TrimSpace(pattern), "\\", "/"), "/")
		if pattern == "" {
			continue
		}
		if _, ok := seen[pattern]; ok {
			continue
		}
		seen[pattern] = struct{}{}
		rule := excludeRule{
			pattern:  pattern,
			hasSlash: strings.Contains(pattern, "/"),
			glob:     strings.ContainsAny(pattern, "*?["),
		}
		if rule.glob {
			re, err := globToRegexp(pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid exclude pattern %q: %w", pattern, err)
			}
			rule.re = re
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// globToRegexp translates a path glob into an anchored regexp. Unlike
// path.Match, `*` does not cross `/` but `**` does, so patterns such as
// `**/secrets` or `data/*/cache` actually match nested paths instead of
// silently excluding nothing.
func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			j := i + 1
			if j < len(pattern) && (pattern[j] == '!' || pattern[j] == '^') {
				j++
			}
			if j < len(pattern) && pattern[j] == ']' {
				j++
			}
			for j < len(pattern) && pattern[j] != ']' {
				j++
			}
			if j >= len(pattern) {
				return nil, fmt.Errorf("unterminated character class in %q", pattern)
			}
			class := pattern[i : j+1]
			if strings.HasPrefix(class, "[!") {
				class = "[^" + class[2:]
			}
			b.WriteString(class)
			i = j
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// validateExcludes rejects malformed glob patterns up front instead of
// silently matching nothing (which would leak files into the bundle).
func validateExcludes(patterns []string) error {
	for _, pattern := range patterns {
		if !strings.ContainsAny(pattern, "*?[") {
			continue
		}
		if _, err := globToRegexp(pattern); err != nil {
			return fmt.Errorf("invalid exclude pattern %q: %w", pattern, err)
		}
	}
	return nil
}

func isExcluded(rel string, rules []excludeRule) bool {
	base := path.Base(rel)
	for _, rule := range rules {
		if rule.glob {
			if rule.re != nil && (rule.re.MatchString(rel) || rule.re.MatchString(base)) {
				return true
			}
			continue
		}
		if rule.hasSlash {
			if rel == rule.pattern || strings.HasPrefix(rel, rule.pattern+"/") {
				return true
			}
			continue
		}
		if base == rule.pattern {
			return true
		}
	}
	return false
}

func copyFile(src, dst string, readOnly bool) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	written, err := io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, err
	}
	mode := fs.FileMode(0o644)
	if readOnly {
		mode = 0o444
	}
	if err := os.Chmod(dst, mode); err != nil {
		return 0, err
	}
	return written, nil
}

func writeBundleMeta(dir, name, content string) error {
	if content == "" {
		content = "unknown"
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o444)
}

func hashBundle(dir string) ([]BundleFile, string, error) {
	entries := []BundleFile{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash == "bundle.json" || relSlash == "bundle-hash.txt" || relSlash == ".hackathonkit-bundle" {
			return nil
		}
		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, BundleFile{
			Path:   filepath.ToSlash(rel),
			SHA256: sum,
			Bytes:  info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	hasher := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(hasher, "%s\x00%s\n", e.Path, e.SHA256)
	}
	return entries, hex.EncodeToString(hasher.Sum(nil)), nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// resolveGitSHA reads .git/HEAD directly so bundling never depends on a git
// binary being installed.
func resolveGitSHA(root string) string {
	gitDir := filepath.Join(root, ".git")
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(head))
	if !strings.HasPrefix(value, "ref: ") {
		if validGitSHA(value) {
			return value
		}
		return ""
	}
	ref := strings.TrimSpace(strings.TrimPrefix(value, "ref: "))
	if !validGitRef(ref) {
		return ""
	}
	refPath := filepath.Join(gitDir, filepath.FromSlash(ref))
	if !strings.HasPrefix(refPath, gitDir+string(filepath.Separator)) {
		return ""
	}
	if data, err := os.ReadFile(refPath); err == nil {
		if sha := strings.TrimSpace(string(data)); validGitSHA(sha) {
			return sha
		}
	}
	packed, err := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(packed), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == ref && validGitSHA(parts[0]) {
			return parts[0]
		}
	}
	return ""
}

func validGitRef(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "/") || strings.Contains(ref, "..") || strings.ContainsAny(ref, "\\:") {
		return false
	}
	for _, r := range ref {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '/', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

func validGitSHA(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// resolveExistingPath resolves symlinks on the longest existing prefix of path
// so containment checks cannot be bypassed by a symlinked directory.
func resolveExistingPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	current := abs
	var suffix []string
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			parts := append([]string{resolved}, suffix...)
			return filepath.Join(parts...)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
	}
}
