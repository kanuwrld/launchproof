package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/kanuwrld/launchproof/internal/project"
	"github.com/kanuwrld/launchproof/internal/tui"
	"github.com/kanuwrld/launchproof/internal/ui"
	"github.com/kanuwrld/launchproof/internal/workflow"
	"github.com/spf13/cobra"
)

func NewRoot(in io.Reader, out, errOut io.Writer, version string) *cobra.Command {
	var plain, noColor, noMotion bool
	root := &cobra.Command{
		Use:          "launchproof [path]",
		Short:        "Know what's ready. Know what's next.",
		Long:         "A local workspace for inspecting projects and making the next step clear.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
	}
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
	root.PersistentFlags().BoolVar(&plain, "plain", false, "Use plain text instead of the interactive interface")
	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable styled output")
	root.PersistentFlags().BoolVar(&noMotion, "no-motion", false, "Disable welcome animation")
	pathFromArgs := func(args []string) (string, error) {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}
		path, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("open project: %w", err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("project path must be a directory")
		}
		return path, nil
	}
	colorEnabled := func() bool {
		return !noColor && !plain && os.Getenv("NO_COLOR") == "" && isTerminal(out) && os.Getenv("TERM") != "dumb"
	}
	welcome := func(cmd *cobra.Command, args []string) error {
		path, err := pathFromArgs(args)
		if err != nil {
			return err
		}
		interactive := !plain && isTerminal(in) && isTerminal(out) && os.Getenv("TERM") != "dumb" && os.Getenv("CI") == "" && os.Getenv("ACCESSIBLE") == ""
		if !interactive {
			_, err := fmt.Fprint(out, ui.PlainWelcome(version, path))
			return err
		}
		model := tui.New(cmd.Context(), path, version, colorEnabled(), !noMotion)
		if err := workflow.RememberProject(path); err != nil {
			fmt.Fprintln(errOut, "Could not update recent projects:", workflow.Redact(err.Error()))
		}
		_, err = tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out), tea.WithContext(cmd.Context())).Run()
		return err
	}
	root.RunE = welcome
	root.AddCommand(&cobra.Command{
		Use: "welcome [path]", Short: "Open the interactive welcome screen",
		Args: cobra.MaximumNArgs(1), RunE: welcome,
	})
	var jsonOutput bool
	inspect := &cobra.Command{
		Use: "inspect [path]", Short: "Inspect manifests, scripts, documents, and Git without running project code",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := pathFromArgs(args)
			if err != nil {
				return err
			}
			report, err := project.Inspect(cmd.Context(), path)
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(out)
				encoder.SetIndent("", "  ")
				return encoder.Encode(report)
			}
			width := 80
			if file, ok := out.(*os.File); ok {
				if w, _, err := term.GetSize(file.Fd()); err == nil {
					width = max(20, min(w, 100))
				}
			}
			lines := ui.ReportLines(report, width, ui.NewTheme(colorEnabled()))
			_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
			return err
		},
	}
	inspect.Flags().BoolVar(&jsonOutput, "json", false, "Write a machine-readable JSON report")
	root.AddCommand(inspect)
	root.AddCommand(workflowCommands(in, out, errOut)...)
	return root
}

func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}
