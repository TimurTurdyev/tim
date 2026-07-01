package tools

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

	"github.com/TimurTurdyev/tim/internal/conv"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var hashNames = []string{"md5", "sha1", "sha256", "sha512", "crc32"}

func printHex(args []string) error {
	vals, err := words(args)
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
}

func printDec(args []string) error {
	vals, err := words(args)
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
}

func base64Text(args []string, decode, urlSafe bool) error {
	s, err := textOrStdin(args)
	if err != nil {
		return err
	}
	if !decode {
		enc := base64.StdEncoding
		if urlSafe {
			enc = base64.RawURLEncoding
		}
		fmt.Println(enc.EncodeToString([]byte(s)))
		return nil
	}
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

func printJWT(args []string) error {
	s, err := textOrStdin(args)
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
			ui.Println(ui.Label(ui.Pad(k, 4)) + " " + t.Format("2006-01-02 15:04:05 MST") + "  " + ui.Dim(relative(t)))
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
}

func printTime(args []string, zone string) error {
	loc := time.Local
	if zone != "" {
		l, err := time.LoadLocation(zone)
		if err != nil {
			return ui.Errorf("зона %s: %v", zone, err)
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
		ui.Println(ui.Dim(relative(t)))
	}
	return nil
}

func printUUIDs(count int, v7 bool) {
	for range max(count, 1) {
		if v7 {
			fmt.Println(conv.UUIDv7(time.Now()))
		} else {
			fmt.Println(conv.UUIDv4())
		}
	}
}

func printHash(args []string, file string) error {
	algo := "sha256"
	if len(args) > 0 && newHash(args[0]) != nil {
		algo, args = args[0], args[1:]
	}
	h := newHash(algo)
	switch {
	case file != "":
		f, err := os.Open(file)
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

func formatJSON(compact bool) error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if compact {
		err = json.Compact(&b, data)
	} else {
		err = json.Indent(&b, data, "", "  ")
	}
	if se, ok := errors.AsType[*json.SyntaxError](err); ok {
		return ui.Errorf("JSON сломан на байте %d: %v", se.Offset, err)
	}
	if err != nil {
		return err
	}
	fmt.Println(strings.TrimRight(b.String(), "\n"))
	return nil
}

func printJSON(raw []byte) {
	var b bytes.Buffer
	if err := json.Indent(&b, raw, "", "  "); err != nil {
		fmt.Println(string(raw))
		return
	}
	fmt.Println(b.String())
}

// relative: "через 3 ч 5 мин" или "2 дн назад" - с точностью до секунд, в отличие от ui.Ago.
func relative(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	if d < 0 {
		return "через " + ui.Duration(-d)
	}
	return ui.Duration(d) + " назад"
}
