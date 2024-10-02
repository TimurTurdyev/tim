package cmd

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var wifiCmd = &cobra.Command{
	Use:   "wifi",
	Short: "Wi-Fi: сеть, канал, стандарт, скорость, сигнал и шум",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := runner.Output("system_profiler", "SPAirPortDataType", "-json")
		if err != nil {
			return ui.Errorf("system_profiler: %v", err)
		}
		w, ok := sysinfo.ParseWifi([]byte(out))
		if !ok {
			ui.Println(ui.Dim("к Wi-Fi не подключено"))
			return nil
		}
		ssid := w.SSID
		if ssid == "<redacted>" {
			ssid = ui.Dim("имя скрыто macOS: дай терминалу доступ к геолокации (Настройки - Конфиденциальность - Службы геолокации)")
		}
		ui.Println(ui.Label("сеть     ") + ssid + ui.Dim("  "+w.Iface))
		ui.Println(ui.Label("канал    ") + w.Channel)
		ui.Println(ui.Label("стандарт ") + w.PHY + ui.Dim("  "+strconv.Itoa(w.Rate)+" Мбит/с"))
		q := w.Quality()
		sig := strconv.Itoa(w.Signal) + " dBm, шум " + strconv.Itoa(w.Noise) + " dBm  " + q
		if q == "слабый" || q == "плохой" {
			sig = ui.Warn(sig)
		}
		ui.Println(ui.Label("сигнал   ") + sig)
		ui.Println(ui.Label("защита   ") + w.Security)
		return nil
	},
}

func init() { root.AddCommand(wifiCmd) }
