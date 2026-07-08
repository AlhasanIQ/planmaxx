package cli

import (
	"fmt"
	"io"

	"github.com/AlhasanIQ/planmaxx/internal/version"

	"github.com/spf13/cobra"
)

func Execute(stdout io.Writer, stderr io.Writer) error {
	return NewRootCommand(stdout, stderr).Execute()
}

func NewRootCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "planmaxx",
		Short:         "Review Codex plans in a local browser workflow",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.SetVersionTemplate(version.String())
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.AddCommand(newReviewCommand(stdout, stderr))
	cmd.AddCommand(newSkillCommand(stdout, stderr))
	cmd.AddCommand(newVersionCommand(stdout))
	return cmd
}

func newVersionCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprint(stdout, version.String())
			return err
		},
	}
}
