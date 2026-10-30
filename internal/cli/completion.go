package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// completeItemIDs suggests short id prefixes with the title as the description.
func (a *App) completeItemIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if err := a.ensure(); err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	prefix := strings.ToLower(strings.TrimSpace(toComplete))
	chosen := make(map[string]bool, len(args))
	for _, arg := range args {
		chosen[strings.ToLower(strings.TrimSpace(arg))] = true
	}
	out := make([]string, 0)
	for _, it := range a.DB.Items() {
		id := shortID(it.ID)
		if chosen[strings.ToLower(id)] {
			continue
		}
		if prefix == "" || strings.HasPrefix(strings.ToLower(id), prefix) {
			out = append(out, id+"\t"+it.Title)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeProjectRefs suggests short project ids with the title.
func (a *App) completeProjectRefs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if err := a.ensure(); err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	prefix := strings.ToLower(strings.TrimSpace(toComplete))
	out := make([]string, 0)
	for _, p := range a.DB.Projects() {
		id := shortID(p.ID)
		if prefix == "" || strings.HasPrefix(strings.ToLower(id), prefix) {
			out = append(out, id+"\t"+p.Title)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func (a *App) completeStatuses(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"todo\tNot started",
		"doing\tIn progress",
		"blocked\tWaiting on something",
		"done\tFinished",
	}, cobra.ShellCompDirectiveNoFileComp
}

func (a *App) completeSortKeys(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"priority\tHighest priority first",
		"due\tSoonest deadline first",
		"updated\tRecently changed first",
		"roi\tImpact for the effort",
		"title\tAlphabetical",
	}, cobra.ShellCompDirectiveNoFileComp
}

func (a *App) completeTypes(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{"Essay", "Coding", "Math", "Research", "Other"}, cobra.ShellCompDirectiveNoFileComp
}
