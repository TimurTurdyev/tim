package cmd

import "github.com/TimurTurdyev/tim/internal/runner"

func confirm(q string) bool { return runner.Confirm(q) }
