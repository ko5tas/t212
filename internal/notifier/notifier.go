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

// Notify sends a Signal message when a position enters or exits the profit threshold.
// entered=true means the position just crossed above the threshold.
// entered=false means it just dropped below.
// profit is the total GBP profit (currentValueGBP - totalBought).
func (n *Notifier) Notify(ticker, name string, entered bool, amount float64) {
	var msg string
	if entered {
		msg = fmt.Sprintf("🟢 %s (%s) is now +£%.2f profit!", name, ticker, amount)
	} else {
		msg = fmt.Sprintf("🔴 %s (%s) is down -£%.2f (10%% loss)", name, ticker, amount)
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
