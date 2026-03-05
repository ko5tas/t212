package notifier

import (
	"fmt"
	"log/slog"
	"os/exec"
)

// Notifier sends Signal messages via a signal-cli subprocess.
type Notifier struct {
	signalCLIPath string
	configPath    string // --config directory (empty = signal-cli default)
	sender        string // -u account (registered on signal-cli)
	recipient     string // destination number
}

// New creates a Notifier. sender is the -u account registered on signal-cli,
// recipient is the number that receives the message. configPath is the --config
// directory (pass "" to use signal-cli's default).
func New(signalCLIPath, sender, recipient, configPath string) *Notifier {
	return &Notifier{signalCLIPath: signalCLIPath, sender: sender, recipient: recipient, configPath: configPath}
}

// Notify sends a Signal message when a position crosses a notification boundary.
// entered=true means the position crossed above the profit threshold.
// entered=false means the return went negative.
// amount is the absolute GBP difference, returnPct is the signed return percentage.
func (n *Notifier) Notify(ticker, name string, entered bool, amount, returnPct float64) {
	var msg string
	if entered {
		msg = fmt.Sprintf("✅▲ %s (%s) +£%.2f (+%.2f%%)", name, ticker, amount, returnPct)
	} else {
		msg = fmt.Sprintf("🟥▼ %s (%s) -£%.2f (%.2f%%)", name, ticker, amount, returnPct)
	}

	args := []string{}
	if n.configPath != "" {
		args = append(args, "--config", n.configPath)
	}
	args = append(args, "-u", n.sender, "send", "-m", msg, n.recipient)

	cmd := exec.Command(n.signalCLIPath, args...)

	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Error("signal-cli failed", "err", err, "output", string(out))
	}
}
