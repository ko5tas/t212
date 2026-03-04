package notifier

import (
	"fmt"
	"log/slog"
	"os/exec"
)

// Notifier sends Signal messages via a signal-cli subprocess.
// The Pi must be registered as a linked device on the user's Signal account.
type Notifier struct {
	signalCLIPath string
	configPath    string // --config directory (empty = signal-cli default)
	number        string // sender = recipient (linked device on own account)
}

// New creates a Notifier. signalCLIPath is the path to the signal-cli binary.
// configPath is the --config directory (pass "" to use signal-cli's default).
func New(signalCLIPath, number, configPath string) *Notifier {
	return &Notifier{signalCLIPath: signalCLIPath, number: number, configPath: configPath}
}

// Notify sends a Signal message when a position enters or exits the profit threshold.
// entered=true means the position just crossed above the threshold.
// entered=false means it just dropped below.
func (n *Notifier) Notify(ticker string, entered bool, profitPerShare float64, currencySymbol string) {
	var msg string
	if entered {
		msg = fmt.Sprintf("📈 %s crossed +%s1/share profit (now +%s%.2f)", ticker, currencySymbol, currencySymbol, profitPerShare)
	} else {
		msg = fmt.Sprintf("📉 %s dropped below +1/share profit", ticker)
	}

	args := []string{}
	if n.configPath != "" {
		args = append(args, "--config", n.configPath)
	}
	args = append(args, "-u", n.number, "send", "-m", msg, n.number)

	cmd := exec.Command(n.signalCLIPath, args...)

	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Error("signal-cli failed", "err", err, "output", string(out))
	}
}
