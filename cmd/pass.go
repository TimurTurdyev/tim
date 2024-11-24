package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/conv"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Полублоки: верхняя половина, нижняя, обе. Записаны кодами, чтобы исходник был набран с клавиатуры.
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
