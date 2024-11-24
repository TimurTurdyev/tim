package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"rsc.io/qr"

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
