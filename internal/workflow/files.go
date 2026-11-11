package workflow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func ProjectRoot(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("open project: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("project must be a directory")
	}
	return absolute, nil
}

func ReadFile(root, name string, limit int64) ([]byte, error) {
	if !filepath.IsLocal(name) {
		return nil, errors.New("file must be inside the project")
	}
	scope, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer scope.Close()
	info, err := scope.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file", name)
	}
	file, err := scope.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds the read limit", name)
	}
	return data, nil
}

func WriteFile(root, name string, data []byte, replace bool) error {
	if !filepath.IsLocal(name) {
		return errors.New("output must be inside the project")
	}
	scope, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer scope.Close()
	parent := filepath.Dir(name)
	parts := strings.Split(filepath.ToSlash(parent), "/")
	current := ""
	for _, part := range parts {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := scope.Lstat(current)
		if err == nil && !info.IsDir() {
			return fmt.Errorf("output directory %s must not be a symlink or file", current)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if errors.Is(err, os.ErrNotExist) {
			if err = scope.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
		}
	}
	if info, err := scope.Lstat(name); err == nil {
		if !replace {
			return fmt.Errorf("%s already exists", name)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular file %s", name)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !replace {
		f, err := scope.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil {
			_ = scope.Remove(name)
			return writeErr
		}
		return closeErr
	}
	token := make([]byte, 8)
	if _, err = rand.Read(token); err != nil {
		return err
	}
	temporary := filepath.Join(parent, ".launchproof-"+hex.EncodeToString(token)+".tmp")
	f, err := scope.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer scope.Remove(temporary)
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return scope.Rename(temporary, name)
}

func gitOutput(ctx context.Context, root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	base := []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-C", root}
	return exec.CommandContext(ctx, "git", append(base, args...)...).Output()
}

func SensitivePath(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return (strings.HasPrefix(base, ".env") && base != ".env.example" && base != ".env.sample") || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") || base == "id_rsa" || base == "id_ed25519"
}

func excluded(name string, patterns []string) bool {
	name = filepath.ToSlash(name)
	if name == StateDir || strings.HasPrefix(name, StateDir+"/") {
		return true
	}
	for _, pattern := range patterns {
		if name == pattern || strings.HasPrefix(name, strings.TrimSuffix(pattern, "/")+"/") {
			return true
		}
		if match, _ := filepath.Match(pattern, name); match {
			return true
		}
	}
	return false
}

func SourceFiles(ctx context.Context, root string, patterns []string) ([]string, error) {
	var files []string
	_, gitErr := gitOutput(ctx, root, "rev-parse", "--show-toplevel")
	_, ignoredErr := gitOutput(ctx, root, "check-ignore", "--quiet", ".")
	if gitErr == nil && ignoredErr != nil {
		data, err := gitOutput(ctx, root, "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", ".")
		if err != nil {
			return nil, fmt.Errorf("list source files: %w", err)
		}
		files = strings.Split(string(data), "\x00")
	} else {
		ignore := map[string]bool{".git": true, "node_modules": true, ".next": true, "dist": true, "build": true, "coverage": true, ".tools": true, "bin": true, "work": true, "vendor": true, ".cache": true, ".venv": true}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if relative == "." {
				return nil
			}
			if d.IsDir() {
				if ignore[d.Name()] || excluded(relative, patterns) {
					return filepath.SkipDir
				}
				return nil
			}
			if len(files) >= 25000 {
				return errors.New("project has more than 25000 source files; narrow the project or add excludes")
			}
			files = append(files, relative)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	result := make([]string, 0, len(files))
	for _, name := range files {
		if name != "" && filepath.IsLocal(name) && !excluded(name, patterns) {
			result = append(result, filepath.ToSlash(name))
		}
	}
	slices.Sort(result)
	result = slices.Compact(result)
	if len(result) > 25000 {
		return nil, errors.New("project has more than 25000 source files; add excludes")
	}
	return result, nil
}

type Snapshot struct {
	Digest string `json:"digest"`
	Files  int    `json:"files"`
	Commit string `json:"commit,omitempty"`
}

func Fingerprint(ctx context.Context, root string, patterns []string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	files, err := SourceFiles(ctx, root, patterns)
	if err != nil {
		return Snapshot{}, err
	}
	scope, err := os.OpenRoot(root)
	if err != nil {
		return Snapshot{}, err
	}
	defer scope.Close()
	hash := sha256.New()
	// A configuration change must invalidate evidence even if the config is ignored.
	if _, err := scope.Stat(ConfigName); err == nil && !slices.Contains(files, ConfigName) {
		files = append(files, ConfigName)
		slices.Sort(files)
	}
	var total int64
	for _, name := range files {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		fmt.Fprintf(hash, "%d:%s\x00", len(name), name)
		info, err := scope.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			io.WriteString(hash, "deleted\x00")
			continue
		}
		if err != nil {
			return Snapshot{}, err
		}
		fmt.Fprintf(hash, "%v\x00", info.Mode())
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := scope.Readlink(name)
			if err != nil {
				return Snapshot{}, err
			}
			fmt.Fprintf(hash, "link:%s\x00", target)
			continue
		}
		if !info.Mode().IsRegular() {
			return Snapshot{}, fmt.Errorf("unsupported source file: %s", name)
		}
		if SensitivePath(name) {
			fmt.Fprintf(hash, "private-metadata:%d:%d\x00", info.Size(), info.ModTime().UnixNano())
			continue
		}
		total += info.Size()
		if total > 256<<20 {
			return Snapshot{}, errors.New("source snapshot exceeds 256 MiB; add excludes")
		}
		file, err := scope.Open(name)
		if err != nil {
			return Snapshot{}, err
		}
		_, copyErr := io.Copy(hash, io.LimitReader(file, info.Size()+1))
		file.Close()
		if copyErr != nil {
			return Snapshot{}, copyErr
		}
		io.WriteString(hash, "\x00")
	}
	commit, _ := gitOutput(ctx, root, "rev-parse", "HEAD")
	return Snapshot{Digest: hex.EncodeToString(hash.Sum(nil)), Files: len(files), Commit: strings.TrimSpace(string(commit))}, nil
}
