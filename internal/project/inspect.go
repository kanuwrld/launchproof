package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const maxManifestSize = 2 << 20

type Report struct {
	SchemaVersion  int        `json:"schema_version"`
	Name           string     `json:"name"`
	Path           string     `json:"path"`
	Stack          []string   `json:"stack"`
	PackageManager string     `json:"package_manager,omitempty"`
	Scripts        []string   `json:"scripts"`
	Documents      []Document `json:"documents"`
	Git            GitInfo    `json:"git"`
	Findings       []Finding  `json:"findings"`
	ChecksExecuted bool       `json:"checks_executed"`
}

type Document struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
}

type Finding struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type GitInfo struct {
	Available bool   `json:"available"`
	Branch    string `json:"branch,omitempty"`
	Commit    string `json:"commit,omitempty"`
	Dirty     bool   `json:"dirty"`
	Error     string `json:"error,omitempty"`
}

type packageManifest struct {
	Name            string            `json:"name"`
	PackageManager  string            `json:"packageManager"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// Inspect reads root-level manifests and Git metadata. It never runs project scripts.
func Inspect(ctx context.Context, path string) (Report, error) {
	root, err := filepath.Abs(path)
	if err != nil {
		return Report{}, fmt.Errorf("resolve project path: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return Report{}, fmt.Errorf("open project: %w", err)
	}
	if !info.IsDir() {
		return Report{}, errors.New("project path must be a directory")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Report{}, fmt.Errorf("resolve project directory: %w", err)
	}
	report := Report{
		SchemaVersion: 1, Name: filepath.Base(root), Path: root,
		Stack: []string{}, Scripts: []string{}, Documents: []Document{},
		Findings: []Finding{},
	}
	data, found, err := readManifest(root, "package.json")
	if err != nil {
		return Report{}, err
	}
	if found {
		var manifest *packageManifest
		if err := json.Unmarshal(data, &manifest); err != nil || manifest == nil {
			return Report{}, errors.New("package.json is not a valid package manifest; correct its JSON and try again")
		}
		if manifest.Name != "" {
			report.Name = manifest.Name
		}
		hasDependency := func(name string) bool {
			_, production := manifest.Dependencies[name]
			_, development := manifest.DevDependencies[name]
			return production || development
		}
		switch {
		case hasDependency("next"):
			report.Stack = append(report.Stack, "Next.js")
		case hasDependency("react"):
			report.Stack = append(report.Stack, "React")
		default:
			report.Stack = append(report.Stack, "Node.js")
		}
		if hasDependency("typescript") || fileExists(root, "tsconfig.json") {
			report.Stack = append(report.Stack, "TypeScript")
		} else {
			report.Stack = append(report.Stack, "JavaScript")
		}
		for name := range manifest.Scripts {
			report.Scripts = append(report.Scripts, name)
		}
		slices.Sort(report.Scripts)
		report.PackageManager = detectManager(root, manifest.PackageManager, &report.Findings)
		if !slices.Contains(report.Scripts, "build") {
			report.Findings = append(report.Findings, Finding{"note", "No build script declared in package.json."})
		}
		hasTests := slices.ContainsFunc(report.Scripts, func(name string) bool {
			return name == "test" || strings.HasPrefix(name, "test:")
		})
		if !hasTests {
			report.Findings = append(report.Findings, Finding{"note", "No test or test:* script declared in package.json."})
		}
	}
	goData, hasGo, err := readManifest(root, "go.mod")
	if err != nil {
		return Report{}, err
	}
	if hasGo {
		report.Stack = append(report.Stack, "Go")
		if !found {
			for _, line := range strings.Split(string(goData), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "module" {
					name := fields[1]
					if unquoted, err := strconv.Unquote(name); err == nil {
						name = unquoted
					}
					report.Name = name
					break
				}
			}
		}
	}
	if len(report.Stack) == 0 {
		report.Findings = append(report.Findings, Finding{
			"note", "No package.json or go.mod found here. Select the application directory in a monorepo.",
		})
	}
	for _, name := range []string{"README.md", "AGENTS.md", ".env.example"} {
		report.Documents = append(report.Documents, Document{Name: name, Present: fileExists(root, name)})
	}
	if !report.Documents[0].Present {
		report.Findings = append(report.Findings, Finding{"note", "Add a README.md with the actual setup and run commands."})
	}
	report.Git = inspectGit(ctx, root)
	return report, nil
}

func readManifest(root, name string) ([]byte, bool, error) {
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%s must be a regular file, not a directory or symbolic link", name)
	}
	// Root-scoped access also prevents symlink escapes if the entry changes after Lstat.
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, false, fmt.Errorf("open project directory: %w", err)
	}
	defer directory.Close()
	file, err := directory.Open(name)
	if err != nil {
		return nil, false, fmt.Errorf("open %s: %w", name, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxManifestSize+1))
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", name, err)
	}
	if len(data) > maxManifestSize {
		return nil, false, fmt.Errorf("%s exceeds the 2 MiB inspection limit", name)
	}
	return data, true, nil
}

func fileExists(root, name string) bool {
	info, err := os.Lstat(filepath.Join(root, name))
	return err == nil && info.Mode().IsRegular()
}

func detectManager(root, declared string, findings *[]Finding) string {
	locks := []struct{ name, manager string }{
		{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"},
		{"bun.lock", "bun"}, {"bun.lockb", "bun"},
		{"package-lock.json", "npm"}, {"npm-shrinkwrap.json", "npm"},
	}
	managers := []string{}
	for _, lock := range locks {
		if fileExists(root, lock.name) && !slices.Contains(managers, lock.manager) {
			managers = append(managers, lock.manager)
		}
	}
	if len(managers) > 1 {
		*findings = append(*findings, Finding{"note", "Multiple package-manager lockfiles found; choose one source of truth."})
	}
	if declared != "" {
		name, _, _ := strings.Cut(declared, "@")
		if slices.Contains([]string{"npm", "pnpm", "yarn", "bun"}, name) {
			if len(managers) > 0 && !slices.Contains(managers, name) {
				*findings = append(*findings, Finding{"note", "The packageManager field disagrees with the detected lockfile."})
			}
			return name
		}
		*findings = append(*findings, Finding{"note", "The packageManager field names an unsupported package manager."})
	}
	if len(managers) == 1 {
		return managers[0]
	}
	return "not determined"
}

func inspectGit(parent context.Context, root string) GitInfo {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	run := func(args ...string) (string, error) {
		base := []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-C", root}
		output, err := exec.CommandContext(ctx, "git", append(base, args...)...).Output()
		return strings.TrimSpace(string(output)), err
	}
	if _, err := run("rev-parse", "--show-toplevel"); err != nil {
		if ctx.Err() != nil {
			return GitInfo{Error: "Git inspection was cancelled or timed out."}
		}
		return GitInfo{}
	}
	result := GitInfo{Available: true}
	result.Branch, _ = run("symbolic-ref", "--quiet", "--short", "HEAD")
	result.Commit, _ = run("rev-parse", "--short", "HEAD")
	if result.Branch == "" && result.Commit != "" {
		result.Branch = "detached"
	}
	status, err := run("status", "--porcelain=v1", "-z", "--untracked-files=normal", "--", ".")
	if err != nil {
		result.Error = "Could not read the working-tree status."
	} else {
		result.Dirty = status != ""
	}
	return result
}
