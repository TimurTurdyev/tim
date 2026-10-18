// Package tools: мелкие утилиты разработчика - числа, base64, JWT, время, UUID, хеши, JSON, URL,
// QR, пароли, cron.
package tools

import (
	"github.com/spf13/cobra"
)

// Commands: команды раздела.
func Commands() []*cobra.Command {
	return []*cobra.Command{
		hexCommand(), decCommand(), b64Command(), jwtCommand(), tsCommand(), uuidCommand(),
		hashCommand(), jsonCommand(), qrCommand(), urlCommand(), passCommand(), cronCommand(),
	}
}

func hexCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "hex <число...>",
		Short: "Десятичное в шестнадцатеричное (и двоичное до 16 бит)",
		RunE:  func(cmd *cobra.Command, args []string) error { return printHex(args) },
	}
}

func decCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "dec <число...>",
		Short: "Шестнадцатеричное (или 0b, 0o) в десятичное",
		RunE:  func(cmd *cobra.Command, args []string) error { return printDec(args) },
	}
}

func b64Command() *cobra.Command {
	var decode, urlSafe bool
	c := &cobra.Command{
		Use:   "b64 [-d] [--url] <текст>",
		Short: "base64: кодировать или с -d раскодировать (std и url сам)",
		RunE:  func(cmd *cobra.Command, args []string) error { return base64Text(args, decode, urlSafe) },
	}
	c.Flags().BoolVarP(&decode, "decode", "d", false, "раскодировать")
	c.Flags().BoolVar(&urlSafe, "url", false, "url-safe алфавит без паддинга")
	return c
}

func jwtCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "jwt <токен>",
		Short: "Раскодировать JWT: header, payload, сроки (подпись не проверяется)",
		RunE:  func(cmd *cobra.Command, args []string) error { return printJWT(args) },
	}
}

func tsCommand() *cobra.Command {
	var zone string
	c := &cobra.Command{
		Use:   "ts [epoch|дата]",
		Short: "Время: epoch <-> дата; без аргумента сейчас",
		RunE:  func(cmd *cobra.Command, args []string) error { return printTime(args, zone) },
	}
	c.Flags().StringVar(&zone, "tz", "", "часовой пояс, например Europe/Moscow")
	return c
}

func uuidCommand() *cobra.Command {
	var v7 bool
	var count int
	c := &cobra.Command{
		Use:   "uuid [-7] [-n N]",
		Short: "Сгенерировать UUID v4 или v7",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { printUUIDs(count, v7) },
	}
	c.Flags().BoolVarP(&v7, "v7", "7", false, "UUID v7 (упорядочен по времени)")
	c.Flags().IntVarP(&count, "count", "n", 1, "сколько штук")
	return c
}

func hashCommand() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:       "hash [md5|sha1|sha256|sha512|crc32] [текст] [-f файл]",
		Short:     "Хеш текста, файла или stdin (по умолчанию sha256)",
		ValidArgs: hashNames,
		RunE:      func(cmd *cobra.Command, args []string) error { return printHash(args, file) },
	}
	c.Flags().StringVarP(&file, "file", "f", "", "файл")
	return c
}

func jsonCommand() *cobra.Command {
	var compact bool
	c := &cobra.Command{
		Use:   "json [-c]",
		Short: "JSON из stdin: красиво или с -c в одну строку",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return formatJSON(compact) },
	}
	c.Flags().BoolVarP(&compact, "compact", "c", false, "в одну строку")
	return c
}

func qrCommand() *cobra.Command {
	var invert bool
	c := &cobra.Command{
		Use:   "qr <текст>",
		Short: "QR-код в терминале: ссылку или пароль от Wi-Fi на телефон",
		RunE:  func(cmd *cobra.Command, args []string) error { return printQR(args, invert) },
	}
	c.Flags().BoolVar(&invert, "invert", false, "для светлой темы терминала")
	return c
}

func urlCommand() *cobra.Command {
	var path bool
	encode := &cobra.Command{
		Use:   "encode <текст> [--path]",
		Short: "Экранировать для query (или --path для сегмента пути)",
		RunE:  func(cmd *cobra.Command, args []string) error { return urlEncode(args, path) },
	}
	encode.Flags().BoolVar(&path, "path", false, "как сегмент пути")
	decode := &cobra.Command{
		Use:   "decode <текст>",
		Short: "Раскодировать %XX и + в пробел",
		RunE:  func(cmd *cobra.Command, args []string) error { return urlDecode(args) },
	}
	parse := &cobra.Command{
		Use:   "parse <url>",
		Short: "Разобрать URL: схема, хост, порт, путь, параметры, фрагмент",
		RunE:  func(cmd *cobra.Command, args []string) error { return urlParse(args) },
	}
	// С адресом без подкоманды - разбор: самое частое, зачем зовут tim url.
	c := &cobra.Command{Use: "url [адрес]", Short: "URL: разобрать, закодировать, раскодировать", Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return urlParse(args)
		}}
	c.AddCommand(encode, decode, parse)
	return c
}

func passCommand() *cobra.Command {
	var o passOptions
	c := &cobra.Command{
		Use:   "pass [-n 20] [-c 1] [--no-symbols] [--no-ambiguous] [--copy]",
		Short: "Сгенерировать пароль (криптослучайный, все классы символов)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return printPasswords(o) },
	}
	c.Flags().IntVarP(&o.length, "length", "n", 20, "длина")
	c.Flags().IntVarP(&o.count, "count", "c", 1, "сколько паролей")
	c.Flags().BoolVar(&o.noSymbols, "no-symbols", false, "только буквы и цифры")
	c.Flags().BoolVar(&o.noAmbiguous, "no-ambiguous", false, "без похожих 0 O o 1 l I")
	c.Flags().BoolVar(&o.copy, "copy", false, "сразу в буфер обмена (его история тоже запомнит пароль)")
	return c
}

func cronCommand() *cobra.Command {
	var runs int
	c := &cobra.Command{
		Use:   `cron "<выражение>" [-n 5]`,
		Short: "Что значит cron-выражение и когда ближайшие запуски",
		Args:  cobra.MinimumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return printCron(args, runs) },
	}
	c.Flags().IntVarP(&runs, "runs", "n", 5, "сколько ближайших запусков")
	return c
}
