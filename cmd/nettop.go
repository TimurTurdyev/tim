package cmd

import (
	"runtime"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	netTopSeconds int
	netTopN       int
)

func rate(bytes int64, d time.Duration) string {
	return ui.Bytes(int64(float64(bytes)/d.Seconds())) + "/с"
}

var netTopCmd = &cobra.Command{
	Use:   "top [--seconds 2] [-n 10]",
	Short: "Кто сейчас качает и отдаёт: процессы по трафику за пару секунд",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS != "darwin" {
			return ui.Errorf("пока только macOS (nettop); на Linux - nethogs")
		}
		d := time.Duration(netTopSeconds) * time.Second
		out, err := runner.Output("nettop", "-P", "-L", "2", "-d", "-s", strconv.Itoa(netTopSeconds), "-x", "-J", "bytes_in,bytes_out")
		if err != nil {
			return ui.Errorf("nettop: %v", err)
		}
		flows := netinfo.ParseNettop(out)
		if len(flows) == 0 {
			ui.Println(ui.Dim("за " + strconv.Itoa(netTopSeconds) + " с сеть никто не трогал"))
			return nil
		}
		ui.Println(ui.Dim(ui.Pad("процесс", 30) + ui.Pad("приём", 14) + "отдача"))
		for i, f := range flows {
			if i == netTopN {
				break
			}
			name := procs.AppName(f.Name)
			ui.Println(ui.Label(ui.Pad(ui.Clip(name, 29), 30)) + ui.Pad(rate(f.In, d), 14) + rate(f.Out, d) + ui.Dim("  pid "+f.PID))
		}
		return nil
	},
}

func init() {
	netTopCmd.Flags().IntVar(&netTopSeconds, "seconds", 2, "сколько секунд мерить")
	netTopCmd.Flags().IntVarP(&netTopN, "count", "n", 10, "сколько строк")
	netCmd.AddCommand(netTopCmd)
}
