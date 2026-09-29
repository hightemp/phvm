package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/hightemp/phvm/internal/core"
	"github.com/hightemp/phvm/internal/log"
)

var aliasListCmd = &cobra.Command{
	Use: "list", Short: "List all aliases", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		list, err := core.NewAliasManager(GetPaths()).List()
		if err != nil {
			return fmt.Errorf("list aliases: %w", err)
		}
		if len(list) == 0 {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "No aliases defined")
			return err
		}
		var names []string
		for name := range list {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s -> %s\n", name, list[name]); err != nil {
				return err
			}
		}
		return nil
	},
}

var aliasCmd = &cobra.Command{
	Use: "alias [name] [version]", Short: "Inspect or create a version alias", Args: cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return aliasListCmd.RunE(cmd, args)
		}
		aliases := core.NewAliasManager(GetPaths())
		if len(args) == 1 {
			version, err := aliases.Get(args[0])
			if err != nil {
				return fmt.Errorf("read alias: %w", err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s -> %s\n", args[0], version)
			return err
		}
		if err := aliases.Set(args[0], args[1]); err != nil {
			return fmt.Errorf("create alias: %w", err)
		}
		log.Success("Alias '%s' -> '%s'", args[0], args[1])
		return nil
	},
}

var unaliasCmd = &cobra.Command{
	Use: "unalias <name>", Short: "Remove a version alias", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := core.NewAliasManager(GetPaths()).Delete(args[0]); err != nil {
			return fmt.Errorf("remove alias: %w", err)
		}
		log.Success("Removed alias '%s'", args[0])
		return nil
	},
}

func init() { aliasCmd.AddCommand(aliasListCmd) }
