package qr

import (
	"fmt"

	qrcode "github.com/skip2/go-qrcode"
)

type QRResult struct {
	PNG         []byte
	DeeplinkURI string
	ConfigText  string
}

func Generate(configText string, size int) (*QRResult, error) {
	if configText == "" {
		return nil, fmt.Errorf("config text must not be empty")
	}

	if size <= 0 {
		size = 512
	}

	png, err := qrcode.Encode(configText, qrcode.Medium, size)
	if err != nil {
		return nil, fmt.Errorf("qr encode: %w", err)
	}

	deeplink, err := BuildDeeplink(configText)
	if err != nil {
		return nil, fmt.Errorf("deeplink: %w", err)
	}

	return &QRResult{
		PNG:         png,
		DeeplinkURI: deeplink,
		ConfigText:  configText,
	}, nil
}
