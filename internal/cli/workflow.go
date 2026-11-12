package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/kanuwrld/launchproof/internal/workflow"
	"github.com/spf13/cobra"
)

func encode(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
func selectedRoot(args []string) (string, error) {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	return workflow.ProjectRoot(path)
}

func workflowCommands(in io.Reader, out, errOut io.Writer) []*cobra.Command {
	var doctorJSON bool
	doctor := &cobra.Command{Use: "doctor [path]", Short: "Diagnose project setup without running scripts", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, err := selectedRoot(args)
		if err != nil {
			return err
		}
		report, err := workflow.Diagnose(cmd.Context(), root)
		if err != nil {
			return err
		}
		if doctorJSON {
			err = encode(out, report)
		} else {
			_, err = fmt.Fprint(out, workflow.DoctorText(report))
		}
		if err != nil {
			return err
		}
		if !report.Healthy() {
			return errors.New("project setup has blocking diagnostics")
		}
		return nil
	}}
	doctor.Flags().BoolVar(&doctorJSON, "json", false, "Write JSON diagnostics")

	var definition workflow.Definition
	var defaults, initJSON bool
	initialize := &cobra.Command{Use: "init [path]", Short: "Create project specs and an explicit check configuration", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, err := selectedRoot(args)
		if err != nil {
			return err
		}
		if !defaults && !initJSON && isTerminal(in) {
			reader := bufio.NewReader(in)
			for _, question := range []struct {
				label string
				value *string
			}{{"What should this project achieve?", &definition.Goal}, {"Who is it for?", &definition.Audience}, {"What must work for you to call it done?", &definition.Acceptance}, {"Any constraints or decisions to preserve?", &definition.Constraints}} {
				if *question.value != "" {
					continue
				}
				fmt.Fprintf(errOut, "%s\n> ", question.label)
				answer, readErr := reader.ReadString('\n')
				if readErr != nil && readErr != io.EOF {
					return readErr
				}
				*question.value = strings.TrimSpace(answer)
			}
		}
		result, err := workflow.Initialize(cmd.Context(), root, definition)
		if err != nil {
			return err
		}
		if initJSON {
			return encode(out, result)
		}
		for _, name := range result.Created {
			fmt.Fprintf(out, "Created   %s\n", name)
		}
		for _, name := range result.Preserved {
			fmt.Fprintf(out, "Preserved %s\n", name)
		}
		_, err = fmt.Fprintln(out, "\nReview launchproof.json, then run launchproof check. Undecided requirements remain explicit in SPEC.md.")
		return err
	}}
	initialize.Flags().BoolVar(&defaults, "defaults", false, "Skip questions; leave unknown requirements explicitly undecided")
	initialize.Flags().BoolVar(&initJSON, "json", false, "Write created and preserved paths as JSON without prompting")
	initialize.Flags().StringVar(&definition.Goal, "goal", "", "Project goal")
	initialize.Flags().StringVar(&definition.Audience, "audience", "", "Intended users")
	initialize.Flags().StringVar(&definition.Acceptance, "acceptance", "", "Acceptance criteria")
	initialize.Flags().StringVar(&definition.Constraints, "constraints", "", "Constraints and existing decisions")

	var checkJSON, dryRun bool
	var only []string
	check := &cobra.Command{Use: "check [path]", Short: "Execute the checks explicitly configured in launchproof.json", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, err := selectedRoot(args)
		if err != nil {
			return err
		}
		cfg, err := workflow.LoadConfig(root)
		if err != nil {
			return err
		}
		if len(only) > 0 {
			requested := map[string]bool{}
			for _, id := range only {
				requested[id] = true
			}
			for i, item := range cfg.Checks {
				if requested[item.ID] {
					delete(requested, item.ID)
				} else {
					cfg.Checks[i].Command = nil
					cfg.Checks[i].Builtin = ""
					cfg.Checks[i].SkipReason = "Not selected for this run."
				}
			}
			if len(requested) > 0 {
				return fmt.Errorf("unknown check id in --only: %v", requested)
			}
		}
		if dryRun {
			if checkJSON {
				return encode(out, cfg.Checks)
			}
			for _, item := range cfg.Checks {
				command := workflow.CommandText(item.Command)
				if item.Builtin != "" {
					command = "builtin: " + item.Builtin
				}
				if item.SkipReason != "" {
					command = "skipped: " + item.SkipReason
				}
				fmt.Fprintf(out, "%-14s %s\n", item.ID, command)
			}
			return nil
		}
		run, err := workflow.Execute(cmd.Context(), root, cfg, func(event workflow.Event) {
			if checkJSON {
				return
			}
			switch event.Kind {
			case "started":
				fmt.Fprintf(out, "→ %-14s running\n", event.Result.ID)
			case "finished":
				fmt.Fprintf(out, "%-16s %-12s %.2fs\n", event.Result.ID, event.Result.Status, float64(event.Result.DurationMS)/1000)
			}
		})
		if err != nil {
			return err
		}
		if checkJSON {
			err = encode(out, run)
		} else {
			_, err = fmt.Fprintf(out, "\nRun saved: .launchproof/runs/%s.json\nView logs with launchproof report; export with --output .launchproof/REPORT.md.\n", run.ID)
			if run.Stale {
				fmt.Fprintf(out, "STALE: %s\n", run.StaleReason)
			}
		}
		if err != nil {
			return err
		}
		if !run.Success() {
			return errors.New("checks did not produce a current successful result")
		}
		return nil
	}}
	check.Flags().BoolVar(&checkJSON, "json", false, "Write one JSON run record (progress is suppressed)")
	check.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the configured commands without executing them")
	check.Flags().StringSliceVar(&only, "only", nil, "Run selected check IDs; record other checks as skipped")

	var reportJSON bool
	var outputPath string
	report := &cobra.Command{Use: "report [path]", Short: "Read the last verification with current/stale evidence", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, err := selectedRoot(args)
		if err != nil {
			return err
		}
		run, err := workflow.Latest(cmd.Context(), root)
		if err != nil {
			return err
		}
		content := []byte(workflow.ReportMarkdown(run))
		if reportJSON {
			content, err = json.MarshalIndent(run, "", "  ")
			if err != nil {
				return err
			}
			content = append(content, '\n')
		}
		if outputPath != "" {
			managed := strings.HasPrefix(filepath.ToSlash(filepath.Clean(outputPath)), workflow.StateDir+"/")
			if err = workflow.WriteFile(root, outputPath, content, managed); err != nil {
				return err
			}
			fmt.Fprintf(errOut, "Saved %s\n", outputPath)
		}
		_, err = out.Write(content)
		return err
	}}
	report.Flags().BoolVar(&reportJSON, "json", false, "Write the saved run and freshness as JSON")
	report.Flags().StringVar(&outputPath, "output", "", "Also save inside the project; existing unmanaged files are preserved")

	handoff := &cobra.Command{Use: "handoff [path]", Short: "Export facts, decisions, tasks and verification for the next session", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, err := selectedRoot(args)
		if err != nil {
			return err
		}
		content, err := workflow.Handoff(cmd.Context(), root)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(out, content)
		return err
	}}
	var docsJSON bool
	docs := &cobra.Command{Use: "docs", Short: "Read and validate project documentation"}
	docsCheck := &cobra.Command{Use: "check [path]", Short: "Validate local Markdown links, anchors, and package script references", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, err := selectedRoot(args)
		if err != nil {
			return err
		}
		result, err := workflow.CheckDocs(cmd.Context(), root)
		if err != nil {
			return err
		}
		if docsJSON {
			err = encode(out, result)
		} else {
			_, err = fmt.Fprint(out, workflow.DocsText(result))
		}
		if err != nil {
			return err
		}
		if len(result.Issues) > 0 {
			return errors.New("documentation contains broken references")
		}
		return nil
	}}
	docsCheck.Flags().BoolVar(&docsJSON, "json", false, "Write validation findings as JSON")
	docs.AddCommand(docsCheck)
	var viewRoot string
	view := &cobra.Command{Use: "view [file.md]", Short: "Read a Markdown file without interpreting it as instructions", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		name := "README.md"
		if len(args) > 0 {
			name = args[0]
		}
		if !strings.EqualFold(".md", filepath.Ext(name)) {
			return errors.New("view accepts Markdown files only")
		}
		root, err := workflow.ProjectRoot(viewRoot)
		if err != nil {
			return err
		}
		data, err := workflow.ReadFile(root, name, 2<<20)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, workflow.Redact(string(data)))
		return err
	}}
	view.Flags().StringVar(&viewRoot, "project", ".", "Project directory containing the Markdown file")
	var projectsJSON bool
	projects := &cobra.Command{Use: "projects", Short: "List recently opened project folders", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		list, err := workflow.RecentProjects()
		if err != nil {
			return err
		}
		if projectsJSON {
			return encode(out, list)
		}
		for _, item := range list {
			fmt.Fprintf(out, "%s  %s\n", item.OpenedAt.Format("2006-01-02"), workflow.CleanText(item.Path))
		}
		return nil
	}}
	projects.Flags().BoolVar(&projectsJSON, "json", false, "Write recent folders as JSON")
	projects.AddCommand(&cobra.Command{Use: "add <path>", Short: "Remember a project folder locally", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error { return workflow.RememberProject(args[0]) }})
	return []*cobra.Command{statusCommand(out), doctor, initialize, check, report, handoff, docs, view, projects}
}
