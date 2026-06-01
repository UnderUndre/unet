//go:build windows

package platform

import (
	"fmt"
	"log/slog"

	toast "github.com/go-toast/toast"
)

// WindowsNotifier implements Notifier via Windows Toast notifications.
type WindowsNotifier struct{}

func NewNotifier() Notifier {
	return &WindowsNotifier{}
}

func (n *WindowsNotifier) Send(title, body string, severity Severity) error {
	icon := ""
	switch severity {
	case SeverityInfo:
		icon = "" // Default info icon
	case SeverityWarning:
		icon = ""
	case SeverityError:
		icon = ""
	}

	notification := toast.Notification{
		AppID:   "unet",
		Title:   title,
		Message: body,
		Icon:    icon,
	}

	if err := notification.Push(); err != nil {
		// Log but don't fail — notifications are best-effort (F8 from review).
		slog.Warn("notifier: toast notification failed (PowerShell may be disabled)", "error", err)
		return fmt.Errorf("notifier: toast push: %w", err)
	}

	slog.Debug("notifier: sent", "title", title, "severity", severity)
	return nil
}
