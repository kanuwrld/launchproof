package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type RecentProject struct {
	Path     string    `json:"path"`
	OpenedAt time.Time `json:"opened_at"`
}

func settingsDir() (string, error) {
	root := os.Getenv("LAUNCHPROOF_HOME")
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(base, "launchproof")
	}
	if info, err := os.Lstat(root); err == nil {
		if !info.IsDir() {
			return "", errors.New("Launchproof settings directory must not be a symlink or file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	} else if err = os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return root, nil
}

func RecentProjects() ([]RecentProject, error) {
	root, err := settingsDir()
	if err != nil {
		return nil, err
	}
	data, err := ReadFile(root, "projects.json", 64<<10)
	if errors.Is(err, os.ErrNotExist) {
		return []RecentProject{}, nil
	}
	if err != nil {
		return nil, err
	}
	var projects []RecentProject
	if err = json.Unmarshal(data, &projects); err != nil {
		return nil, err
	}
	if len(projects) > 12 {
		projects = projects[:12]
	}
	return projects, nil
}

func RememberProject(path string) error {
	root, err := ProjectRoot(path)
	if err != nil {
		return err
	}
	projects, err := RecentProjects()
	if err != nil {
		return err
	}
	next := []RecentProject{{Path: root, OpenedAt: time.Now().UTC()}}
	for _, project := range projects {
		if project.Path != root && len(next) < 12 {
			next = append(next, project)
		}
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	directory, err := settingsDir()
	if err != nil {
		return err
	}
	return WriteFile(directory, "projects.json", append(data, '\n'), true)
}
