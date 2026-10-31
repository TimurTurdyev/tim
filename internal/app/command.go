package app

import (
	"github.com/spf13/cobra"
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
