package cli

import "github.com/spf13/cobra"

func newDoctorCmd() *cobra.Command {
	return stub("doctor", "Check the machine and repository for COWFlow requirements", cobra.NoArgs)
}
