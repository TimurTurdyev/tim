package app

import (
	"strconv"

	"github.com/spf13/cobra"
)

const (
	// Parent: пометка команды раздела, которая встаёт под команду другого раздела (cert new под cert).
	Parent = "tim/parent"
	// JSON: пометка команды, у которой есть вывод в JSON. С --json у других корень отвечает ошибкой:
	// скрипт или агент ждёт JSON, а получил бы текст.
	JSON = "tim/json"
	// External: своя команда tim-<имя>, значение - путь к ней. Экран не запускает её у себя: ей нужен терминал.
	External = "tim/external"
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

// ExitCode: своя команда завершилась с этим кодом. Сообщение она напечатала сама, корень молчит и
// выходит с тем же кодом.
type ExitCode int

func (e ExitCode) Error() string { return "код выхода " + strconv.Itoa(int(e)) }
