package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxScanDepth caps how deep a category scan descends, counted in path
// segments below the task directory: "evidences/01-antes/sub/file.png" is four
// and is read, one level deeper is not. Evidence bundles are shallow by
// convention, and the cap keeps a stray symlink loop or a checked-out
// dependency tree from turning a scan into a full-disk walk.
const maxScanDepth = 4

// noiseDirs are directories a knowledge scan never descends into, whatever
// category they appear under.
var noiseDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
}

// noiseFiles are files a knowledge scan never records.
var noiseFiles = map[string]bool{
	".DS_Store": true,
}

// File is one entry found under a task's category directory.
type File struct {
	// RelPath is the file relative to the task directory, always starting with
	// its category and always slash-separated:
	// "evidences/01-login-antes/01-pantalla-login.png".
	RelPath string
	// Category is the category directory the file was found under.
	Category Category
	// Kind is the coarse type derived from the extension.
	Kind Kind
	// Size is the file size in bytes, reported even when the file is skipped.
	Size int64
	// SHA256 is the hex digest of the content, empty for a skipped entry.
	SHA256 string
	// ModTime is the file's modification time in UTC. It is what an importer
	// records as the evidence's captured_at.
	ModTime time.Time
	// Proves is what the file demonstrates, resolved from the sibling
	// manifest.json, then the sibling README's first H1, then RelPath. It is
	// never empty.
	Proves string
	// Skipped reports that the entry was recorded without reading it.
	Skipped bool
	// SkipReason is SkipRestricted or SkipOversize when Skipped is set.
	SkipReason string
}

// TaskDir is one task folder of the vault.
type TaskDir struct {
	Project string
	// Dir is the absolute path of the task folder.
	Dir string
	// JiraKey is the ticket the folder name leads with, empty for the
	// satellite and catch-all folders that carry none.
	JiraKey string
	// Slug is the folder name with its ticket removed.
	Slug string
	// Title is the first H1 of the folder's README, falling back to the folder
	// name when there is none.
	Title string
	// Summary is the README's first paragraph.
	Summary string
	// State is the state the README's "**Estado:**" line maps to, empty when
	// it names none of the four known states or carries no such line.
	State string
	// PendingNote is the note after a "Con pendientes" state.
	PendingNote string
	// Files are every recorded entry across the eleven categories, ordered by
	// RelPath.
	Files []File
	// BenchmarkRuns are the JSON files under benchmarks/, in the same order.
	BenchmarkRuns []RunFile
}

// ProjectDir is one project folder of the vault.
type ProjectDir struct {
	Slug string
	// Dir is the absolute path of the project folder.
	Dir string
	// Title is the first H1 of the project README.
	Title string
	// Description is the project README's first paragraph.
	Description string
	// Tasks are the task folders, ordered by folder name.
	Tasks []TaskDir
	// Warnings name everything the scan refused to read: restricted paths, and
	// task folders that could not be scanned.
	Warnings []string
}

// ScanProject walks one project folder: its README, then every task folder
// under it. Folders prefixed with "_" or "." are skipped, which is how the
// vault marks quarantine, out-of-domain and architecture material that is not
// a task.
func ScanProject(root, slug string, o Options) (ProjectDir, error) {
	o = o.normalized()
	dir := filepath.Join(root, slug)
	restricted, err := IsRestricted(dir, o.Restricted)
	if err != nil {
		return ProjectDir{}, err
	}
	if restricted {
		return ProjectDir{}, fmt.Errorf("%w: %s", ErrRestrictedPath, dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ProjectDir{}, fmt.Errorf("vault: read project %s: %w", dir, err)
	}

	project := ProjectDir{Slug: slug, Dir: dir}
	if header, err := parseReadmeHeader(filepath.Join(dir, "README.md")); err == nil {
		project.Title = header.Title
		project.Description = header.Summary
	}
	if project.Title == "" {
		project.Title = slug
	}

	for _, entry := range entries {
		if !entry.IsDir() || skipName(entry.Name()) {
			continue
		}
		task, err := ScanTask(root, slug, entry.Name(), o)
		if err != nil {
			project.Warnings = append(project.Warnings,
				fmt.Sprintf("task %s/%s skipped: %v", slug, entry.Name(), err))
			continue
		}
		for _, f := range task.Files {
			if f.Skipped && f.SkipReason == SkipRestricted {
				project.Warnings = append(project.Warnings,
					fmt.Sprintf("restricted path skipped: %s/%s/%s", slug, entry.Name(), f.RelPath))
			}
		}
		project.Tasks = append(project.Tasks, task)
	}
	return project, nil
}

// ScanTask walks one task folder: its README header and each of the eleven
// categories, plus the benchmark runs under benchmarks/.
func ScanTask(root, project, task string, o Options) (TaskDir, error) {
	o = o.normalized()
	dir := filepath.Join(root, project, task)
	restricted, err := IsRestricted(dir, o.Restricted)
	if err != nil {
		return TaskDir{}, err
	}
	if restricted {
		return TaskDir{}, fmt.Errorf("%w: %s", ErrRestrictedPath, dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return TaskDir{}, fmt.Errorf("vault: read task %s: %w", dir, err)
	}
	if !info.IsDir() {
		return TaskDir{}, fmt.Errorf("vault: %s is not a task directory", dir)
	}

	out := TaskDir{Project: project, Dir: dir}
	out.JiraKey, out.Slug = ParseTaskDirName(task)
	if header, err := parseReadmeHeader(filepath.Join(dir, "README.md")); err == nil {
		out.Title = header.Title
		out.Summary = header.Summary
		out.State = header.State
		out.PendingNote = header.PendingNote
	}
	if out.Title == "" {
		out.Title = out.Slug
	}

	for _, category := range allCategories {
		files, err := ScanCategory(dir, category, o)
		if err != nil {
			return TaskDir{}, err
		}
		out.Files = append(out.Files, files...)
	}

	for _, f := range out.Files {
		if f.Category != CategoryBenchmarks || f.Kind != KindJSON || f.Skipped {
			continue
		}
		if strings.EqualFold(filepath.Base(f.RelPath), "benchmark_map.json") {
			continue
		}
		run, err := ParseRunJSON(filepath.Join(dir, filepath.FromSlash(f.RelPath)))
		if err != nil {
			return TaskDir{}, err
		}
		run.RelPath = f.RelPath
		out.BenchmarkRuns = append(out.BenchmarkRuns, run)
	}
	return out, nil
}

// ScanCategory walks one category directory of a task. An absent category is
// not an error: most tasks only fill in a few of the eleven.
//
// Entries are returned ordered by RelPath, so two scans of an unchanged tree
// produce byte-identical results. Restricted and oversize entries are recorded
// as skipped rather than dropped, so a caller can tell "nothing there" from
// "something there we refused to read".
func ScanCategory(taskDir string, c Category, o Options) ([]File, error) {
	o = o.normalized()
	root := filepath.Join(taskDir, string(c))
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("vault: read category %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, nil
	}
	var files []File
	if err := scanDir(root, string(c), c, o, &files); err != nil {
		return nil, err
	}
	return files, nil
}

// scanDir walks one directory of a category, recursing until maxScanDepth.
// relDir is the directory's path relative to the task directory.
func scanDir(dir, relDir string, c Category, o Options, out *[]File) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("vault: read %s: %w", dir, err)
	}
	meta := loadDirMeta(dir)

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			if skipName(name) || noiseDirs[name] {
				continue
			}
		} else if noiseFiles[name] {
			continue
		}
		relPath := relDir + "/" + name
		fullPath := filepath.Join(dir, name)

		restricted, err := IsRestricted(fullPath, o.Restricted)
		if err != nil {
			return err
		}
		if restricted {
			// Recorded, never opened and never descended into: the caller
			// needs to know something is there without anything reading it.
			*out = append(*out, File{
				RelPath:    relPath,
				Category:   c,
				Kind:       KindOf(name),
				Proves:     relPath,
				Skipped:    true,
				SkipReason: SkipRestricted,
			})
			continue
		}

		// entry.IsDir reports on the link itself, so a symlink to a directory
		// needs a stat to be recognised as one.
		info, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // a dangling symlink proves nothing
			}
			return fmt.Errorf("vault: stat %s: %w", fullPath, err)
		}
		if info.IsDir() {
			if depthOf(relPath) >= maxScanDepth {
				continue
			}
			if err := scanDir(fullPath, relPath, c, o, out); err != nil {
				return err
			}
			continue
		}
		if depthOf(relPath) > maxScanDepth {
			continue
		}

		file := File{
			RelPath:  relPath,
			Category: c,
			Kind:     KindOf(name),
			Size:     info.Size(),
			ModTime:  info.ModTime().UTC(),
			Proves:   meta.proves(name, relPath),
		}
		if info.Size() > o.MaxBytes {
			file.Skipped = true
			file.SkipReason = SkipOversize
			*out = append(*out, file)
			continue
		}
		digest, err := hashFile(fullPath)
		if err != nil {
			return err
		}
		file.SHA256 = digest
		*out = append(*out, file)
	}
	return nil
}

// depthOf counts the path segments of a task-relative path.
func depthOf(relPath string) int {
	return strings.Count(relPath, "/") + 1
}

// skipName reports whether a directory entry is one the vault marks as not
// part of the knowledge tree: the "_quarantine" style folders and anything
// hidden.
func skipName(name string) bool {
	return strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".")
}

// dirMeta is what a directory says about the files it holds.
type dirMeta struct {
	manifest map[string]string
	readmeH1 string
}

// proves resolves what a file demonstrates: the sibling manifest entry first,
// then the sibling README's first H1, then the file's own relative path. The
// last fallback is what guarantees the answer is never empty.
func (m dirMeta) proves(name, relPath string) string {
	if p := strings.TrimSpace(m.manifest[name]); p != "" {
		return p
	}
	if m.readmeH1 != "" {
		return m.readmeH1
	}
	return relPath
}

// loadDirMeta reads the manifest.json and README.md a directory may carry.
// Both are optional and a malformed one is simply not used: a broken manifest
// must not stop a scan, only lower the quality of the "proves" it derives.
func loadDirMeta(dir string) dirMeta {
	return dirMeta{
		manifest: loadManifest(filepath.Join(dir, "manifest.json")),
		readmeH1: readmeTitle(dir),
	}
}

// loadManifest reads an evidence manifest, indexed by file name. Both shapes
// the vault uses are accepted: a name mapped straight to a sentence, and a
// name mapped to an object whose "proves" field carries it.
func loadManifest(path string) map[string]string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	out := make(map[string]string, len(entries))
	for name, value := range entries {
		var sentence string
		if err := json.Unmarshal(value, &sentence); err == nil {
			out[name] = sentence
			continue
		}
		var object struct {
			Proves string `json:"proves"`
		}
		if err := json.Unmarshal(value, &object); err == nil {
			out[name] = object.Proves
		}
	}
	return out
}

// hashFile streams the file through sha256 so a large evidence bundle never
// lands in memory whole.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("vault: open %s: %w", path, err)
	}
	defer f.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		return "", fmt.Errorf("vault: hash %s: %w", path, err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
