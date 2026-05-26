package cli

import "github.com/spf13/cobra"

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "drifthound",
		Short:        "Detect AWS resources that aren't tracked by Terraform code",
		SilenceUsage: true,
	}
	cmd.AddCommand(newScanCommand())
	return cmd
}
