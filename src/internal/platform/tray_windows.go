//go:build windows

package platform

import (
	"context"
	"log/slog"

	"fyne.io/systray"
	"github.com/underundre/unet/internal/platform/icons"
)

// systrayImpl implements Tray using fyne.io/systray.
type systrayImpl struct {
	menuItems map[string]*systray.MenuItem
	onReady   []func()
	onExit    []func()
}

func NewTray() Tray {
	return &systrayImpl{
		menuItems: make(map[string]*systray.MenuItem),
	}
}

func (s *systrayImpl) Run(ctx context.Context) error {
	// Ensure systray.Quit() is called when context is cancelled,
	// otherwise the tray icon lingers in the taskbar.
	go func() {
		<-ctx.Done()
		systray.Quit()
	}()
	systray.Run(s.onReadyFunc(ctx), s.onExitFunc())
	return nil
}

func (s *systrayImpl) SetIcon(state IconState) {
	var data []byte
	switch state {
	case IconGreen:
		data = icons.Green
	case IconYellow:
		data = icons.Yellow
	case IconRed:
		data = icons.Red
	default:
		data = icons.Red
	}
	systray.SetIcon(data)
}

func (s *systrayImpl) SetTooltip(text string) {
	systray.SetTooltip(text)
}

func (s *systrayImpl) SetMenu(items []MenuItem) {
	// systray doesn't support dynamic menu rebuild easily.
	// Store items for onReady. Menu is set up once in onReady.
	s.menuItems = make(map[string]*systray.MenuItem)
	for _, item := range items {
		s.addMenuItem(item)
	}
}

func (s *systrayImpl) SetMenuItemLabel(id, label string) {
	if mi, ok := s.menuItems[id]; ok {
		mi.SetTitle(label)
	}
}

func (s *systrayImpl) SetMenuItemDisabled(id string, disabled bool) {
	if mi, ok := s.menuItems[id]; ok {
		if disabled {
			mi.Disable()
		} else {
			mi.Enable()
		}
	}
}

func (s *systrayImpl) SetMenuItemChecked(id string, checked bool) {
	if mi, ok := s.menuItems[id]; ok {
		mi.Check()
		if !checked {
			mi.Uncheck()
		}
	}
}

// addMenuItem adds a single menu item to the systray menu.
func (s *systrayImpl) addMenuItem(item MenuItem) {
	if len(item.SubItems) > 0 {
		parent := systray.AddMenuItem(item.Label, "")
		s.menuItems[item.ID] = parent
		for _, sub := range item.SubItems {
			child := parent.AddSubMenuItem(sub.Label, "")
			s.menuItems[sub.ID] = child
			if sub.OnClick != nil {
				go func(fn func()) {
					for range child.ClickedCh {
						fn()
					}
				}(sub.OnClick)
			}
		}
		return
	}

	var mi *systray.MenuItem
	if item.ID == "separator" {
		systray.AddSeparator()
		return
	}

	mi = systray.AddMenuItem(item.Label, "")
	s.menuItems[item.ID] = mi

	if item.Disabled {
		mi.Disable()
	}
	if item.Checked {
		mi.Check()
	}
	if item.OnClick != nil {
		go func(fn func()) {
			for range mi.ClickedCh {
				fn()
			}
		}(item.OnClick)
	}
}

func (s *systrayImpl) onReadyFunc(ctx context.Context) func() {
	return func() {
		slog.Debug("tray: onReady")
		// Set default red icon on startup.
		s.SetIcon(IconRed)
		s.SetTooltip("unet: starting...")

		for _, fn := range s.onReady {
			fn()
		}
	}
}

func (s *systrayImpl) onExitFunc() func() {
	return func() {
		slog.Debug("tray: onExit")
		for _, fn := range s.onExit {
			fn()
		}
	}
}
