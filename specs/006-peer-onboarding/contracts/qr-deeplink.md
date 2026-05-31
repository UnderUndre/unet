# QR Code & Deeplink Specification

**Spec**: `specs/006-peer-onboarding/spec.md`
**Created**: 2026-05-28

---

## QR Code Generation

### Library

`github.com/skip2/go-qrcode` — pure Go, zero CGO, generates PNG bytes directly.

### Parameters

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Content | Full WireGuard client config text | Must include all AmneziaWG obfuscation params |
| Size | 256×256 pixels (default) | Sufficient for mobile scanning at arm's length. 128 and 512 also supported. |
| Error correction | Level M (~15%) | Balances recovery capability vs QR density. Level L (7%) too fragile; Level Q (25%) makes QR too dense for the config length. |
| Encoding | Binary (auto-detect) | Config text contains special chars; binary mode avoids encoding issues. |

### QR Content Format

The QR encodes the **complete WireGuard client configuration** as plain text. This is the standard format recognized by official WireGuard apps:

```ini
[Interface]
PrivateKey = <client-private-key>
Address = <client-wg-ip>/32
MTU = 1280
DNS = 1.1.1.1

[Peer]
PublicKey = <server-public-key>
PresharedKey = <optional-psk>
AllowedIPs = 0.0.0.0/0
Endpoint = <vps-public-ip>:<wg-udp-port>
PersistentKeepalive = 25

# AmneziaWG obfuscation parameters
Jc = <value>
Jmin = <value>
Jmax = <value>
S1 = <value>
S2 = <value>
S3 = <value>
S4 = <value>
H1 = <value>
H2 = <value>
H3 = <value>
H4 = <value>
I1 = <value>
I2 = <value>
I3 = <value>
I4 = <value>
I5 = <value>
```

**Important**: The AmneziaWG parameters (Jc, Jmin, Jmax, S1-S4, H1-H4, I1-I5) MUST be included. Without them, the WireGuard tunnel will not connect (AmneziaWG rejects connections missing obfuscation params).

### QR PNG Response

API returns QR as base64-encoded PNG:

```json
{
  "qr_png_base64": "iVBORw0KGgoAAAANSUhEUg..."
}
```

Client decodes and renders as `<img src="data:image/png;base64,{qr_png_base64}">`.

---

## Deeplink URI

### Format

```
wireguard://import?config=<base64url-encoded-config>
```

**Components**:
- Scheme: `wireguard://` — registered by official WireGuard apps on Android and iOS.
- Path: `/import` — triggers config import in the app.
- Query param `config`: Base64url-encoded (no padding) WireGuard config text.

### Construction

```
1. config_text = full WG client config (same as QR content above)
2. config_base64 = base64url_encode(config_text)  // RFC 4648 §5, no padding
3. deeplink_uri = "wireguard://import?config=" + config_base64
```

### Platform Support

| Platform | URI Scheme | Behavior | Tested |
|----------|-----------|----------|--------|
| Android (official WG app) | `wireguard://` | Opens app, imports config, shows tunnel name. User must activate manually. | Yes (spec reference) |
| iOS (official WG app) | `wireguard://` | Opens app, imports config. Same behavior as Android. | Yes (spec reference) |
| Windows (official WG app) | No URI scheme | Desktop WG app doesn't register URL handler. Must use .conf file import. | N/A |
| macOS (official WG app) | No URI scheme | Same as Windows. | N/A |
| Third-party WG clients | Varies | May not support `wireguard://`. Fallback: .conf file. | N/A |

### Fallback: Manual Import

When deeplink fails (app not installed, platform unsupported), provide:

1. **Download .conf file**: `GET /invite/{peerId}/download` returns the config as a downloadable file.
2. **Copyable config text**: Plain text of the full config in a `<textarea>` or code block.
3. **Platform-specific instructions**:
   - **Android**: "Open WireGuard app → '+' → 'Scan from QR code' (point camera at QR) OR 'Import from file' (select downloaded .conf)"
   - **iOS**: "Open WireGuard app → 'Add tunnel' → 'Import from file' (select downloaded .conf)"
   - **Windows**: "Open WireGuard → 'Import tunnel(s) from file' → select downloaded .conf"
   - **macOS**: Same as Windows.
   - **Linux**: `sudo wg setconf wg0 <(cat downloaded.conf)` OR use NetworkManager WG plugin.

---

## Landing Page Behavior

The invite landing page (`GET /invite/{peerId}`) must handle multiple scenarios:

### Scenario 1: Mobile device with WG app installed

1. User taps invite link in messaging app.
2. Browser opens landing page.
3. Page detects mobile OS (Android/iOS) via User-Agent.
4. Page shows QR code prominently (scan with WG app camera).
5. Below QR: "Or open directly in WireGuard app" button → navigates to `wireguard://import?config=...`.
6. Deeplink opens WG app → config imported.

### Scenario 2: Mobile device without WG app

1. Same as above, but deeplink fails (no app registered for `wireguard://`).
2. Page shows QR code (user can scan with another device's WG app).
3. Below QR: "Download WireGuard app" link (Google Play / App Store based on OS detection).
4. Below that: "Download config file" button → triggers .conf download.
5. Instructions: "Install WireGuard → Import from file."

### Scenario 3: Desktop browser

1. User opens invite link on desktop.
2. Page shows QR code (for scanning with phone's WG app).
3. Below QR: "Download config file" button.
4. Below that: copyable config text in code block.
5. Instructions for desktop WG import.

### OS Detection

```javascript
function detectOS(userAgent) {
  if (/Android/i.test(userAgent)) return 'android';
  if (/iPhone|iPad|iPod/i.test(userAgent)) return 'ios';
  if (/Windows/i.test(userAgent)) return 'windows';
  if (/Mac/i.test(userAgent)) return 'macos';
  if (/Linux/i.test(userAgent)) return 'linux';
  return 'unknown';
}
```

### Download Links

| OS | URL |
|----|-----|
| Android | `https://play.google.com/store/apps/details?id=com.wireguard.android` |
| iOS | `https://apps.apple.com/us/app/wireguard/id1441195209` |
| Windows | `https://download.wireguard.com/windows-client/wireguard-installer.exe` |
| macOS | `https://apps.apple.com/us/app/wireguard/id1451685025` |
| Linux | Package manager specific. Link to `https://www.wireguard.com/install/` |

---

## QR Regeneration

QR codes can be regenerated at any time for existing peers via `POST /v1/peers/{id}/qr`. The config text is deterministic (same peer = same config), so regenerated QRs are identical. No state is stored — purely on-demand generation.

**Use case**: User lost the QR display, wants to re-scan. Or user is on a different device and needs the QR again.
