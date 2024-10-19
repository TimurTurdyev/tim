package cmd

import (
	"fmt"
	"net/url"
	"sort"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Полублоки: верхняя половина, нижняя, обе. Записаны кодами, чтобы исходник был набран с клавиатуры.
var urlPath bool

var urlCmd = &cobra.Command{
	Use:   "url",
	Short: "URL: encode, decode, parse",
}

var urlEncodeCmd = &cobra.Command{
	Use:   "encode <текст> [--path]",
	Short: "Экранировать для query (или --path для сегмента пути)",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := stdinText(args)
		if err != nil {
			return err
		}
		if urlPath {
			fmt.Println(url.PathEscape(s))
		} else {
			fmt.Println(url.QueryEscape(s))
		}
		return nil
	},
}

var urlDecodeCmd = &cobra.Command{
	Use:   "decode <текст>",
	Short: "Раскодировать %XX и + в пробел",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := stdinText(args)
		if err != nil {
			return err
		}
		out, err := url.QueryUnescape(s)
		if err != nil {
			if out, err = url.PathUnescape(s); err != nil {
				return ui.Errorf("не раскодировать: %v", err)
			}
		}
		fmt.Println(out)
		return nil
	},
}

var urlParseCmd = &cobra.Command{
	Use:   "parse <url>",
	Short: "Разобрать URL: схема, хост, порт, путь, параметры, фрагмент",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := stdinText(args)
		if err != nil {
			return err
		}
		u, err := url.Parse(s)
		if err != nil {
			return err
		}
		row := func(k, v string) {
			if v != "" {
				ui.Println(ui.Label(ui.Pad(k, 9)) + " " + v)
			}
		}
		row("схема", u.Scheme)
		if u.User != nil {
			row("логин", u.User.Username())
		}
		row("хост", u.Hostname())
		row("порт", u.Port())
		row("путь", u.Path)
		q := u.Query()
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			for _, v := range q[k] {
				ui.Println(ui.Label("  "+ui.Pad(k, 18)) + " " + v)
			}
		}
		row("фрагмент", u.Fragment)
		return nil
	},
}
