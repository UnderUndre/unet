package traylogic

import (
	"github.com/underundre/unet/internal/platform"
)

// VPSEntry represents a VPS the tray can connect to.
type VPSEntry struct {
	ID   string
	Name string
	Host string
}

// buildVPSSubMenu creates a VPS switching sub-menu if multiple VPS configured.
// Currently returns empty (single-VPS). Will be extended when multi-VPS lands.
func (a *App) buildVPSSubMenu() []platform.MenuItem {
	// Single VPS: no sub-menu needed.
	return nil
}
