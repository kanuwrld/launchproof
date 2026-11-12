package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/kanuwrld/launchproof/internal/project"
)

const ConfigName = "launchproof.json"
const StateDir = ".launchproof"

type Definition struct {
	Goal        string `json:"goal"`
	Audience    string `json:"audience"`
	Acceptance  string `json:"acceptance"`
	Constraints string `json:"constraints"`
}

type Check struct {
	ID             string   `json:"id"`
	Command        []string `json:"command,omitempty"`
	Builtin        string   `json:"builtin,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
	SkipReason     string   `json:"skip_reason,omitempty"`
}

type Requirement struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Checks      []string `json:"checks"`
}

type Config struct {
	SchemaVersion int           `json:"schema_version"`
	Project       Definition    `json:"project"`
	Checks        []Check       `json:"checks"`
	Exclude       []string      `json:"exclude,omitempty"`
	Requirements  []Requirement `json:"requirements,omitempty"`
}

func LoadConfig(root string) (Config, error) {
	data, err := ReadFile(root, ConfigName, 1<<20)
	if err != nil {
		return Config{}, fmt.Errorf("read %s (run launchproof init first): %w", ConfigName, err)
	}
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("invalid %s: %w", ConfigName, err)
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return cfg, errors.New("configuration must contain exactly one JSON object")
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.SchemaVersion != 1 {
		return errors.New("unsupported configuration schema_version; expected 1")
	}
	if len(c.Checks) == 0 || len(c.Checks) > 24 {
		return errors.New("configure between 1 and 24 checks")
	}
	idPattern := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,47}$`)
	seen := map[string]bool{}
	for _, check := range c.Checks {
		if !idPattern.MatchString(check.ID) || seen[check.ID] {
			return fmt.Errorf("invalid or duplicate check id %q", check.ID)
		}
		seen[check.ID] = true
		modes := 0
		if len(check.Command) > 0 {
			modes++
		}
		if check.Builtin != "" {
			modes++
		}
		if check.SkipReason != "" {
			modes++
		}
		if modes != 1 {
			return fmt.Errorf("check %s needs exactly one of command, builtin, or skip_reason", check.ID)
		}
		if check.Builtin != "" && check.Builtin != "docs" {
			return fmt.Errorf("unsupported builtin %q", check.Builtin)
		}
		if len(check.Command) > 64 || (len(check.Command) > 0 && strings.TrimSpace(check.Command[0]) == "") {
			return fmt.Errorf("invalid command for %s", check.ID)
		}
		for _, arg := range check.Command {
			if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
				return fmt.Errorf("invalid argument in %s", check.ID)
			}
		}
		if check.TimeoutSeconds < 0 || check.TimeoutSeconds > 3600 {
			return fmt.Errorf("timeout for %s must be between 0 and 3600 seconds", check.ID)
		}
	}
	for _, pattern := range c.Exclude {
		if !filepath.IsLocal(pattern) || strings.Contains(pattern, "\\") {
			return fmt.Errorf("exclude must be a relative slash-separated path or glob: %q", pattern)
		}
		if _, err := filepath.Match(pattern, "probe"); err != nil {
			return fmt.Errorf("invalid exclude pattern: %w", err)
		}
	}
	for _, value := range []string{c.Project.Goal, c.Project.Audience, c.Project.Acceptance, c.Project.Constraints} {
		if len(value) > 8000 {
			return errors.New("project answers must be at most 8000 bytes each")
		}
	}
	if len(c.Requirements) > 100 {
		return errors.New("configure at most 100 requirements")
	}
	requirements := map[string]bool{}
	for _, requirement := range c.Requirements {
		if !idPattern.MatchString(requirement.ID) || requirements[requirement.ID] {
			return fmt.Errorf("invalid or duplicate requirement id %q", requirement.ID)
		}
		requirements[requirement.ID] = true
		if strings.TrimSpace(requirement.Description) == "" || len(requirement.Description) > 8000 {
			return fmt.Errorf("requirement %s needs a description of at most 8000 bytes", requirement.ID)
		}
		linked := map[string]bool{}
		for _, id := range requirement.Checks {
			if !seen[id] || linked[id] {
				return fmt.Errorf("requirement %s references an unknown or duplicate check %q", requirement.ID, id)
			}
			linked[id] = true
		}
	}
	return nil
}

func DefaultConfig(report project.Report, definition Definition) Config {
	cfg := Config{SchemaVersion: 1, Project: definition, Checks: []Check{}}
	if strings.TrimSpace(definition.Acceptance) != "" {
		cfg.Requirements = []Requirement{{ID: "acceptance", Description: definition.Acceptance, Checks: []string{}}}
	}
	if slices.Contains(report.Stack, "Go") {
		for _, id := range []string{"vet", "test", "build"} {
			cfg.Checks = append(cfg.Checks, Check{ID: id, Command: []string{"go", id, "./..."}, TimeoutSeconds: 120})
		}
	}
	if len(report.Scripts) > 0 || report.PackageManager != "" {
		for _, id := range []string{"lint", "typecheck", "test", "build"} {
			script := id
			if id == "typecheck" && !slices.Contains(report.Scripts, id) && slices.Contains(report.Scripts, "type-check") {
				script = "type-check"
			}
			if id == "test" && !slices.Contains(report.Scripts, id) && slices.Contains(report.Scripts, "test:unit") {
				script = "test:unit"
			}
			check := Check{ID: id, TimeoutSeconds: 120}
			if slices.Contains(report.Stack, "Go") {
				check.ID = "node-" + id
			}
			if !slices.Contains(report.Scripts, script) {
				check.SkipReason = "No matching package.json script declared."
			} else if !slices.Contains([]string{"npm", "pnpm", "yarn", "bun"}, report.PackageManager) {
				check.SkipReason = "Choose a package manager and configure the command."
			} else {
				check.Command = []string{report.PackageManager, "run", script}
			}
			cfg.Checks = append(cfg.Checks, check)
		}
	}
	cfg.Checks = append(cfg.Checks, Check{ID: "docs", Builtin: "docs", TimeoutSeconds: 30})
	return cfg
}
