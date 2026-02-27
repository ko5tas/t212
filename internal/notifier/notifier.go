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
	number        string // sender = recipient (linked device on own account)
}

// New creates a Notifier. signalCLIPath is the path to the signal-cli binary.
func New(signalCLIPath, number string) *Notifier {
	return &Notifier{signalCLIPath: signalCLIPath, number: number}
}

// Notify sends a Signal message when a position enters or exits the profit threshold.
// entered=true means the position just crossed above the threshold.
// entered=false means it just dropped below.
func (n *Notifier) Notify(ticker string, entered bool, profitPerShare float64) {
	var msg string
	if entered {
		msg = fmt.Sprintf("📈 %s crossed +£1/share profit (now +£%.2f)", ticker, profitPerShare)
	} else {
		msg = fmt.Sprintf("📉 %s dropped below +£1/share profit", ticker)
	}

	cmd := exec.Command(
		n.signalCLIPath,
		"-u", n.number,
		"send",
		"-m", msg,
		n.number,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Error("signal-cli failed", "err", err, "output", string(out))
	}
}
