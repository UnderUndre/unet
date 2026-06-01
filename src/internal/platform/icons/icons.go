// Package icons embeds tray icon PNG assets.
package icons

import _ "embed"

//go:embed icon_green.png
var Green []byte

//go:embed icon_yellow.png
var Yellow []byte

//go:embed icon_red.png
var Red []byte
