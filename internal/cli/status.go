package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/kanuwrld/launchproof/internal/workflow"
	"github.com/spf13/cobra"
)

func statusCommand(out io.Writer) *cobra.Command {
	var jsonOutput, strict bool
	command := &cobra.Command{
		Use: "status [path]", Short: "Show source changes, evidence gaps, and next actions without running checks",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := selectedRoot(args)
			if err != nil {
				return err
			}
			report, err := workflow.Status(cmd.Context(), root)
			if err != nil {
				return err
			}
			if jsonOutput {
				err = encode(out, report)
			} else {
				_, err = fmt.Fprint(out, workflow.StatusText(report))
			}
			if err != nil {
				return err
			}
			if strict && !report.Passes() {
				return errors.New("configured verification is incomplete; review the status and next actions")
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "Write one read-only status document")
	command.Flags().BoolVar(&strict, "strict", false, "Exit nonzero unless every configured check and declared requirement has current passing evidence")
	return command
}
