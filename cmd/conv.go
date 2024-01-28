package cmd

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/conv"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// argsOrStdin: аргументы, а без них содержимое stdin, если он не терминал.
func argsOrStdin(args []string) ([]string, error) {
	if len(args) > 0 {
		return args, nil
	}
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice != 0 {
		return nil, errors.New("нужен аргумент или ввод через stdin")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(data)), nil
}

func stdinText(args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice != 0 {
		return "", errors.New("нужен аргумент или ввод через stdin")
	}
	data, err := io.ReadAll(os.Stdin)
	return strings.TrimRight(string(data), "\n"), err
}

var hexCmd = &cobra.Command{
	Use:   "hex <число...>",
	Short: "Десятичное в шестнадцатеричное (и двоичное до 16 бит)",
	RunE: func(cmd *cobra.Command, args []string) error {
		vals, err := argsOrStdin(args)
		if err != nil {
			return err
		}
		for _, v := range vals {
			n, err := conv.ParseNumber(v, 10)
			if err != nil {
				return err
			}
			line := conv.Hex(n)
			if b, ok := conv.Bin(n); ok {
				line += "  " + ui.Dim(b)
			}
			fmt.Println(line)
		}
		return nil
	},
}

var decCmd = &cobra.Command{
	Use:   "dec <число...>",
	Short: "Шестнадцатеричное (или 0b, 0o) в десятичное",
	RunE: func(cmd *cobra.Command, args []string) error {
		vals, err := argsOrStdin(args)
		if err != nil {
			return err
		}
		for _, v := range vals {
			n, err := conv.ParseNumber(v, 16)
			if err != nil {
				return err
			}
			fmt.Println(n.String())
		}
		return nil
	},
}

var (
	b64Decode bool
	b64URL    bool
)

var b64Cmd = &cobra.Command{
	Use:   "b64 [-d] [--url] <текст>",
	Short: "base64: кодировать или с -d раскодировать (std и url сам)",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := stdinText(args)
		if err != nil {
			return err
		}
		if b64Decode {
			out, err := conv.Base64Decode(s)
			if err != nil {
				return err
			}
			_, err = os.Stdout.Write(out)
			if len(out) > 0 && out[len(out)-1] != '\n' {
				fmt.Println()
			}
			return err
		}
		enc := base64.StdEncoding
		if b64URL {
			enc = base64.RawURLEncoding
		}
		fmt.Println(enc.EncodeToString([]byte(s)))
		return nil
	},
}

var jwtCmd = &cobra.Command{
	Use:   "jwt <токен>",
	Short: "Раскодировать JWT: header, payload, сроки (подпись не проверяется)",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := stdinText(args)
		if err != nil {
			return err
		}
		j, err := conv.DecodeJWT(s)
		if err != nil {
			return err
		}
		ui.Println(ui.Label("header"))
		printJSON(j.Header)
		ui.Println(ui.Label("payload"))
		printJSON(j.Claims)
		for _, k := range []string{"iat", "nbf", "exp"} {
			if t, ok := j.Times[k]; ok {
				ui.Println(ui.Label(ui.Pad(k, 4)) + " " + t.Format("2006-01-02 15:04:05 MST") + "  " + ui.Dim(ago(t)))
			}
		}
		if expired, ok := j.Expired(time.Now()); ok {
			if expired {
				ui.Println(ui.Bad("токен истёк"))
			} else {
				ui.Println(ui.Info("токен действует"))
			}
		}
		ui.Println(ui.Dim("подпись не проверялась"))
		return nil
	},
}

func printJSON(raw []byte) {
	var b bytes.Buffer
	if err := json.Indent(&b, raw, "", "  "); err != nil {
		fmt.Println(string(raw))
		return
	}
	fmt.Println(b.String())
}

func ago(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	if d < 0 {
		return "через " + human(-d)
	}
	return human(d) + " назад"
}

func human(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d дн", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%d ч %d мин", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%d мин", int(d.Minutes()))
	}
	return fmt.Sprintf("%d с", int(d.Seconds()))
}

var tsZone string

var tsCmd = &cobra.Command{
	Use:   "ts [epoch|дата]",
	Short: "Время: epoch <-> дата; без аргумента сейчас",
	RunE: func(cmd *cobra.Command, args []string) error {
		loc := time.Local
		if tsZone != "" {
			l, err := time.LoadLocation(tsZone)
			if err != nil {
				return ui.Errorf("зона %s: %v", tsZone, err)
			}
			loc = l
		}
		t := time.Now().In(loc)
		if len(args) > 0 {
			v, err := conv.ParseTime(strings.Join(args, " "), loc)
			if err != nil {
				return err
			}
			t = v.In(loc)
		}
		ui.Println(ui.Label("epoch ") + fmt.Sprint(t.Unix()) + "  " + ui.Dim(fmt.Sprintf("мс %d", t.UnixMilli())))
		ui.Println(ui.Label("local ") + t.Format(time.RFC3339))
		ui.Println(ui.Label("utc   ") + t.UTC().Format(time.RFC3339))
		if len(args) > 0 {
			ui.Println(ui.Dim(ago(t)))
		}
		return nil
	},
}

var (
	uuidV7    bool
	uuidCount int
)

var uuidCmd = &cobra.Command{
	Use:   "uuid [-7] [-n N]",
	Short: "Сгенерировать UUID v4 или v7",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		for range max(uuidCount, 1) {
			if uuidV7 {
				fmt.Println(conv.UUIDv7(time.Now()))
			} else {
				fmt.Println(conv.UUIDv4())
			}
		}
		return nil
	},
}

var hashFile string

var hashCmd = &cobra.Command{
	Use:       "hash [md5|sha1|sha256|sha512|crc32] [текст] [-f файл]",
	Short:     "Хеш текста, файла или stdin (по умолчанию sha256)",
	ValidArgs: []string{"md5", "sha1", "sha256", "sha512", "crc32"},
	RunE: func(cmd *cobra.Command, args []string) error {
		algo := "sha256"
		if len(args) > 0 && newHash(args[0]) != nil {
			algo, args = args[0], args[1:]
		}
		h := newHash(algo)
		switch {
		case hashFile != "":
			f, err := os.Open(hashFile)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			if _, err := io.Copy(h, f); err != nil {
				return err
			}
		case len(args) > 0:
			h.Write([]byte(strings.Join(args, " ")))
		default:
			st, err := os.Stdin.Stat()
			if err != nil || st.Mode()&os.ModeCharDevice != 0 {
				return errors.New("нужен текст, -f файл или stdin")
			}
			if _, err := io.Copy(h, os.Stdin); err != nil {
				return err
			}
		}
		fmt.Println(hex.EncodeToString(h.Sum(nil)))
		return nil
	},
}

func newHash(name string) hash.Hash {
	switch name {
	case "md5":
		return md5.New()
	case "sha1":
		return sha1.New()
	case "sha256":
		return sha256.New()
	case "sha512":
		return sha512.New()
	case "crc32":
		return crc32.NewIEEE()
	}
	return nil
}

var jsonCompact bool

var jsonCmd = &cobra.Command{
	Use:   "json [-c]",
	Short: "JSON из stdin: красиво или с -c в одну строку",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		var b bytes.Buffer
		if jsonCompact {
			err = json.Compact(&b, data)
		} else {
			err = json.Indent(&b, data, "", "  ")
		}
		if err != nil {
			var se *json.SyntaxError
			if errors.As(err, &se) {
				return ui.Errorf("JSON сломан на байте %d: %v", se.Offset, err)
			}
			return err
		}
		fmt.Println(strings.TrimRight(b.String(), "\n"))
		return nil
	},
}

func init() {
	b64Cmd.Flags().BoolVarP(&b64Decode, "decode", "d", false, "раскодировать")
	b64Cmd.Flags().BoolVar(&b64URL, "url", false, "url-safe алфавит без паддинга")
	tsCmd.Flags().StringVar(&tsZone, "tz", "", "часовой пояс, например Europe/Moscow")
	uuidCmd.Flags().BoolVarP(&uuidV7, "v7", "7", false, "UUID v7 (упорядочен по времени)")
	uuidCmd.Flags().IntVarP(&uuidCount, "count", "n", 1, "сколько штук")
	hashCmd.Flags().StringVarP(&hashFile, "file", "f", "", "файл")
	jsonCmd.Flags().BoolVarP(&jsonCompact, "compact", "c", false, "в одну строку")
	root.AddCommand(hexCmd, decCmd, b64Cmd, jwtCmd, tsCmd, uuidCmd, hashCmd, jsonCmd)
}
