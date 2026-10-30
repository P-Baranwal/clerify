package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) guideCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "guide",
		Short:   "A short walkthrough of the daily workflow",
		Long:    "Print a compact tutorial for the capture, review, and finish loop.",
		Example: "  clerify guide",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a.printf(`clerify in five steps

1. Capture
   clerify add "Write report" --due friday --est 3
   clerify add "essay due friday, about 5h"      # natural language works

2. See what's next
   clerify today
   clerify ls

3. Pick something
   clerify start #1        # or use an id prefix: clerify start 7f3a1b
   clerify show #1

4. Finish it
   clerify done #1 --actual 4

5. Fix a mistake
   clerify undo

Other useful things
   clerify project new --title "Website redesign"
   clerify wbs --brief "..." --apply
   clerify report
   clerify init            # optional AI scoring

Run "clerify <command> --help" for flags and examples.
`)
			return nil
		},
	}
}
