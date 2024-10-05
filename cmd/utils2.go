package cmd

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"rsc.io/qr"

	"github.com/TimurTurdyev/tim/internal/conv"
	"github.com/TimurTurdyev/tim/internal/cronx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Полублоки: верхняя половина, нижняя, обе. Записаны кодами, чтобы исходник был набран с клавиатуры.
const (
	blockUpper = "\u2580"
	blockLower = "\u2584"
	blockFull  = "\u2588"
)

var qrCmd = &cobra.Command{
	Use:   "qr <текст>",
	Short: "QR-код в терминале: ссылку или пароль от Wi-Fi на телефон",
	RunE: func(cmd *cobra.Command, args []string) error {
		text, err := stdinText(args)
		if err != nil {
			return err
		}
		code, err := qr.Encode(text, qr.M)
		if err != nil {
			return ui.Errorf("QR: %v", err)
		}
		// Рамка из светлых модулей нужна сканеру. На тёмном фоне светлые модули - блоки, тёмные - пробелы; --invert наоборот.
		const quiet = 2
		size := code.Size + 2*quiet
		black := func(x, y int) bool {
			x, y = x-quiet, y-quiet
			return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
		}
		var b strings.Builder
		for y := 0; y < size; y += 2 {
			for x := 0; x < size; x++ {
				top, bottom := black(x, y) == qrInvert, black(x, y+1) == qrInvert
				switch {
				case top && bottom:
					b.WriteString(blockFull)
				case top:
					b.WriteString(blockUpper)
				case bottom:
					b.WriteString(blockLower)
				default:
					b.WriteString(" ")
				}
			}
			b.WriteString("\n")
		}
		fmt.Print(b.String())
		return nil
	},
}

var qrInvert bool

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

var (
	passLen       int
	passCount     int
	passNoSymbols bool
	passNoAmbig   bool
)

var passCmd = &cobra.Command{
	Use:   "pass [-n 20] [-c 1] [--no-symbols] [--no-ambiguous]",
	Short: "Сгенерировать пароль (криптослучайный, все классы символов)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		for range max(passCount, 1) {
			p, err := conv.Password(passLen, !passNoSymbols, passNoAmbig)
			if err != nil {
				return ui.Errorf("%v", err)
			}
			fmt.Println(p)
		}
		return nil
	},
}

var cronRuns int

var cronCmd = &cobra.Command{
	Use:   `cron "<выражение>" [-n 5]`,
	Short: "Что значит cron-выражение и когда ближайшие запуски",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		expr := strings.Join(args, " ")
		s, err := cronx.Parse(expr)
		if err != nil {
			return ui.Errorf("%s: %v", expr, err)
		}
		ui.Println(ui.Label(s.Describe()))
		for _, t := range s.Next(time.Now(), cronRuns) {
			ui.Println("  " + t.Format("Mon 2006-01-02 15:04") + ui.Dim("  "+ago(t)))
		}
		return nil
	},
}

func init() {
	qrCmd.Flags().BoolVar(&qrInvert, "invert", false, "для светлой темы терминала")
	urlEncodeCmd.Flags().BoolVar(&urlPath, "path", false, "как сегмент пути")
	urlCmd.AddCommand(urlEncodeCmd, urlDecodeCmd, urlParseCmd)
	passCmd.Flags().IntVarP(&passLen, "length", "n", 20, "длина")
	passCmd.Flags().IntVarP(&passCount, "count", "c", 1, "сколько паролей")
	passCmd.Flags().BoolVar(&passNoSymbols, "no-symbols", false, "только буквы и цифры")
	passCmd.Flags().BoolVar(&passNoAmbig, "no-ambiguous", false, "без похожих 0 O o 1 l I")
	cronCmd.Flags().IntVarP(&cronRuns, "runs", "n", 5, "сколько ближайших запусков")
	root.AddCommand(qrCmd, urlCmd, passCmd, cronCmd)
}
