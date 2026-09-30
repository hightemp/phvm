package cli

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/core"
)

func completeInstallPHP(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return matchingCompletions([]string{"latest", "stable"}, prefix), cobra.ShellCompDirectiveNoFileComp
}

func completeInstalledPHP(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	paths := GetPaths()
	installed := core.NewInstalledManager(paths)
	versions, err := installed.List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	aliases, err := core.NewAliasManager(paths).List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	for name := range aliases {
		if _, err := installed.Resolve(name); err == nil {
			versions = append(versions, name)
		}
	}
	for _, name := range []string{"latest", "stable"} {
		if _, err := installed.Resolve(name); err == nil {
			versions = append(versions, name)
		}
	}
	return matchingCompletions(versions, prefix), cobra.ShellCompDirectiveNoFileComp
}

func completeUninstallPHP(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	versions, err := core.NewInstalledManager(GetPaths()).List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return matchingCompletions(versions, prefix), cobra.ShellCompDirectiveNoFileComp
}

func matchingCompletions(values []string, prefix string) []string {
	matches := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if strings.HasPrefix(value, prefix) && !seen[value] {
			matches = append(matches, value)
			seen[value] = true
		}
	}
	sort.Strings(matches)
	return matches
}
