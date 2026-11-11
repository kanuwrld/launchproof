package workflow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kanuwrld/launchproof/internal/project"
)

type Diagnostic struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}
type DoctorReport struct {
	Items []Diagnostic `json:"items"`
}

func (r DoctorReport) Healthy() bool {
	for _, item := range r.Items {
		if item.Status == "error" {
			return false
		}
	}
	return true
}

func Diagnose(ctx context.Context, root string) (DoctorReport, error) {
	result := DoctorReport{Items: []Diagnostic{}}
	report, err := project.Inspect(ctx, root)
	if err != nil {
		return result, err
	}
	runtime := func(name string) {
		resolved, err := exec.LookPath(name)
		if err != nil {
			result.Items = append(result.Items, Diagnostic{name, "error", "Not found on PATH. Install the project's required version."})
		} else {
			result.Items = append(result.Items, Diagnostic{name, "passed", resolved})
		}
	}
	if slices.Contains(report.Stack, "Go") {
		runtime("go")
	}
	if report.PackageManager != "" {
		runtime("node")
		if slices.Contains([]string{"npm", "pnpm", "yarn", "bun"}, report.PackageManager) {
			runtime(report.PackageManager)
		} else {
			result.Items = append(result.Items, Diagnostic{"package manager", "warning", "Declare packageManager or keep one consistent lockfile."})
		}
		if info, err := os.Stat(filepath.Join(root, "node_modules")); err != nil || !info.IsDir() {
			result.Items = append(result.Items, Diagnostic{"dependencies", "warning", "node_modules is absent; dependencies may need installation (Yarn PnP may not use this directory)."})
		} else {
			result.Items = append(result.Items, Diagnostic{"dependencies", "passed", "node_modules is present; versions were not validated."})
		}
	}
	if len(report.Stack) == 0 {
		result.Items = append(result.Items, Diagnostic{"stack", "warning", "No supported root manifest. Configure checks explicitly."})
	}
	cfg, err := LoadConfig(root)
	if err != nil {
		result.Items = append(result.Items, Diagnostic{"configuration", "warning", err.Error()})
	} else {
		result.Items = append(result.Items, Diagnostic{"configuration", "passed", fmt.Sprintf("%d configured checks", len(cfg.Checks))})
		for _, check := range cfg.Checks {
			if check.SkipReason != "" {
				result.Items = append(result.Items, Diagnostic{check.ID, "warning", check.SkipReason})
			}
		}
	}
	for _, finding := range report.Findings {
		result.Items = append(result.Items, Diagnostic{"project", "warning", finding.Message})
	}
	for _, document := range report.Documents {
		status, detail := "passed", "Present"
		if !document.Present {
			status, detail = "warning", "Not found; add this document if the project needs it."
		}
		result.Items = append(result.Items, Diagnostic{document.Name, status, detail})
	}
	if info, err := os.Lstat(filepath.Join(root, ".env")); err == nil {
		detail := "Present; values were not read."
		if !info.Mode().IsRegular() {
			detail = "Not a regular file; values were not read."
		}
		result.Items = append(result.Items, Diagnostic{".env", "note", detail})
	}
	return result, nil
}

func DoctorText(report DoctorReport) string {
	var b strings.Builder
	b.WriteString("PROJECT DOCTOR\n\n")
	for _, item := range report.Items {
		fmt.Fprintf(&b, "[%s] %s\n  %s\n\n", strings.ToUpper(item.Status), CleanText(item.Name), CleanText(item.Detail))
	}
	b.WriteString("Diagnostics inspect setup; they do not install dependencies or run project scripts.\n")
	return b.String()
}
