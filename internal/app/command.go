package app

import (
	"github.com/spf13/cobra"
)

const (
	// Parent: пометка команды раздела, которая встаёт под команду другого раздела (cert new под cert).
	Parent = "tim/parent"
	// JSON: пометка команды, у которой есть вывод в JSON. С --json у других корень отвечает ошибкой:
	// скрипт или агент ждёт JSON, а получил бы текст.
	JSON = "tim/json"
)

// Under помечает команду: корень подвесит её под parent.
func Under(parent string, c *cobra.Command) *cobra.Command {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[Parent] = parent
	return c
}

// WithJSON помечает команду: общий флаг --json у неё работает.
func WithJSON(c *cobra.Command) *cobra.Command {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[JSON] = "yes"
	return c
}
