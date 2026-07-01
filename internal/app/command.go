package app

import (
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
)

// Parent: пометка команды раздела, которая встаёт под команду другого раздела (cert new под cert).
const Parent = "tim/parent"

// Under помечает команду: корень подвесит её под parent.
func Under(parent string, c *cobra.Command) *cobra.Command {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[Parent] = parent
	return c
}

// ProjectNames: автодополнение имён проектов. Cobra зовёт его до PersistentPreRunE, поэтому конфиг
// читается здесь сам.
func ProjectNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c, err := config.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	names := make([]string, 0, len(c.Projects))
	for _, p := range c.Projects {
		names = append(names, p.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
