package qr

import (
	"encoding/base64"
	"fmt"
)

func BuildDeeplink(configText string) (string, error) {
	if configText == "" {
		return "", fmt.Errorf("config text must not be empty")
	}

	encoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(configText))
	return "wireguard://import?config=" + encoded, nil
}
