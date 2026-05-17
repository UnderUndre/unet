# Code Review: 001-init Implementation

**Reviewer**: Claude (post-implementation code review, persona: Valera)
**Reviewed at**: 2026-05-17T16:00:00Z (approximate; src/ files dated 2026-05-17 14:08-16:09)
**Scope**: `src/cmd/unet/`, `src/internal/**`, `src/tests/**` against `specs/001-init/` and `.gemini/review.md`
**Standards applied**: `.github/instructions/coding/copilot-instructions.md`, `.specify/memory/constitution.md` v1.4.0
**Total LOC reviewed**: ~6,250 lines Go (35 files) + 2,150 lines tests

---

## Executive Summary

Имплементация написана **до прочтения `.gemini/review.md`** (review.md datestamp — 18:00; код — 14:08-16:09). Из 5 архитектурных issues, поднятых review.md, **4 проигнорированы**, 1 покрыт частично. Дополнительно при прицельном чтении выявлено **3 критических несоответствия спеке** (S1/S2/Q1) и **3 quality issue**.

| Verdict | Count |
|---------|-------|
| **CRITICAL** (blocks production, contradicts spec) | **2** |
| **HIGH** (correctness / security / known-broken) | **4** |
| **MEDIUM** (degraded correctness, edge cases) | **5** |
| **LOW** (cosmetic, future tech-debt) | **3** |
| ✅ **GOOD** (correct, well-designed) | 7 highlights |

**Не сливать в main без устранения CRITICAL.**

---

## Section 1 — Findings from `.gemini/review.md` (architectural)

`.gemini/review.md` поднял 5 архитектурных проблем. Трассировка против кода:

| # | review.md finding | Code state | Severity |
|---|-------------------|------------|----------|
| **RV1** | Pause-container needed: `network_mode: "service:..."` привязывает Caddy к amnezia lifecycle. Restart amnezia → Caddy теряет netstack. | ❌ **NOT IMPLEMENTED** — [`src/internal/provisioner/compose.go:50`](src/internal/provisioner/compose.go) использует `network_mode: "service:unet-amnezia-awg"` exactly как warned. Никакого pause-container. | **HIGH** |
| **RV2** | Client-side TCP-proxy чтоб юзер мог биндить `127.0.0.1`, а не `0.0.0.0`. Иначе locallhost экспонится на любую сеть, к которой подключён. | ❌ **NOT IMPLEMENTED** — нет ни одного `net.Listen` на `tunnel.LocalIP` в [`src/internal/daemon/`](src/internal/daemon/) или [`src/internal/tunnel/`](src/internal/tunnel/). Спека и quickstart всё ещё мандатят `0.0.0.0`. Security hole. | **HIGH** |
| **RV3** | `mktemp` + `flock` на server-side peer-add чтоб два конкурирующих демона не клобберили друг друга. | ❌ **NOT IMPLEMENTED** — [`src/internal/tunnel/peer.go:249`](src/internal/tunnel/peer.go) использует hardcoded `/tmp/awg0-strip.conf`. Никакого `mktemp`, никакого `flock`. In-process `sync.Mutex` в `manager.go` защищает только ОДИН демон, не cross-daemon. | **HIGH** |
| **RV4** | CGNAT subnet `100.64.0.0/10` (RFC 6598, как Tailscale) вместо `10.8.x.x`. Иначе конфликты с OpenVPN/корпоратив. | ❌ **NOT IMPLEMENTED** — [`src/internal/provisioner/awg_init.go:154`](src/internal/provisioner/awg_init.go): `return fmt.Sprintf("10.8.%d.0/24", x)`. | **MEDIUM** |
| **RV5** | Windows awg-quick не работает как Linux CLI — там Windows Service + wintun + UAC. | ⚠ **PARTIALLY** — [`src/internal/tunnel/awg.go:241-258`](src/internal/tunnel/awg.go) имеет `discoverWindows()`, но **идентичен `discoverDarwin()`** — просто `awg show interfaces`. **Главный invocation `awg-quick up` GOOS-agnostic** (`exec.Command("awg-quick", "up", conf)`). На Windows это не запустится без UAC + Service. Real fix (per review.md): использовать `golang.zx2c4.com/wireguard/windows` или встроить `amneziawg-go` userspace. | **HIGH** |

---

## Section 2 — Spec compliance gaps (drift code↔spec)

После того как ты применил Phases 1-4 remediation к спеке, код **не обновили** под обновлённую спеку:

### S1 — **CRITICAL**: mTLS bootstrap использует уязвимый pattern (antigravity F2)

**Где**: [`src/internal/proxy/caddy_mtls.go:127-251`](src/internal/proxy/caddy_mtls.go) (function `BootstrapMTLS`)

**Что обнаружено**:
```go
// Line 217:
acURL := baseURL + "/config/admin/remote/access_control"
// ... POST to Caddy admin API to register public_keys
```

**Спека после Phases 1-4** ([`contracts/caddy-api.md`](specs/001-init/contracts/caddy-api.md) "mTLS Provisioning Flow"):
> Key principle (post-F2): mTLS client public keys are registered **via SSH + `docker exec`** editing of Caddy's config file on the server, NOT via the Caddy admin API.

**Реальность кода**: использует **именно тот pattern, который antigravity F2 объявил сломанным**. Первый peer регистрируется через admin API → Caddy флипается в mTLS-only → второй peer не может POST'нуть свой ключ → permanent lockout.

**Severity**: CRITICAL — это **regression к pre-Phase-2 архитектуре**, спека и контракт ушли вперёд, код остался.

**Fix**: переписать `BootstrapMTLS` использовать `ssh.Client` + `docker exec unet-caddy sh -c 'cat > /config/caddy/autosave.json'` + `docker exec unet-caddy caddy reload --config /config/caddy/autosave.json --adapter json`. См. [`appendix-peer-add-flow.md`](specs/001-init/appendix-peer-add-flow.md) §2.6 для канонической последовательности команд.

### S2 — **HIGH**: single-label subdomain validation не соответствует FR-009/FR-012

**Где**: [`src/internal/daemon/api_ports.go:21,91-95`](src/internal/daemon/api_ports.go)

**Что обнаружено**:
```go
var subdomainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
// ...
if !subdomainRe.MatchString(req.Subdomain) {
    writeError(w, http.StatusBadRequest, "invalid_subdomain", ...)
}
```

Проблемы:
1. **Regex не допускает точки вообще** — значит API ожидает только leftmost label (`app`), без zone. Это работает для Cloudflare wildcard (single-label by design), но **полностью ломает manual DNS mode**, где multi-level разрешён.
2. **Error code `invalid_subdomain`** — спека требует `invalid_subdomain_depth` для multi-level в Cloudflare mode со structured `context` payload (`labelsUnderBase`, `remediation`). См. [`contracts/daemon-api.md`](specs/001-init/contracts/daemon-api.md) "Response 400 (subdomain depth — Cloudflare mode only)".
3. **DNS mode не проверяется** — нет ветки `if cfg.DNS.Mode == "cloudflare" { ... single-label ... } else { ... multi-level OK ... }`.

**Fix**: ввести two-phase validation:
   - Phase 1: общий regex с допуском точек (`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
   - Phase 2: если `dns.mode == "cloudflare"`, посчитать labels-under-baseDomain → если >1, вернуть `400 invalid_subdomain_depth` с context-payload.

### S3 — **MEDIUM**: FR-010 sub-2 local stale-state reconciliation не реализован

**Спека**: на daemon startup, BEFORE acting on persisted `tunnel.status`, выполнить `awg show <iface>` и если interface не существует / handshake stale → reset status в `"disconnected"`.

**Что обнаружено**: [`src/cmd/unet/startup.go`](src/cmd/unet/startup.go) делает CheckAwgPath (✅) и acquireLock (✅), но **не выполняет state-reconciliation против persisted config**. Нет вызова `awg show` для проверки фактического состояния туннеля при старте.

**Fix**: добавить `ReconcileStartupState(ctx, cfgMgr, awgCli)` в startup, до запуска HTTP server.

---

## Section 3 — Quality issues (logic, тесты)

### Q1 — **HIGH**: DELETE `/api/ports/{id}` **никогда не работает** для нормально созданных портов

**Где**: [`src/internal/daemon/api_ports.go`](src/internal/daemon/api_ports.go)

**Симптом**:
- `handleCreate` (line 126): `portID := uuid.New().String()` — возвращает UUID
- `handleRemove` (line 211): `if strings.HasPrefix(portID, "port-")` — ищет только `port-N` формат

UUID никогда не имеет префикс `port-` → `idx` остаётся `-1` → возврат `404 not_found`. **Каждый DELETE для UUID-id возвращает 404**, даже если порт реально существует.

Дополнительно: `ExposedPort` структура (config.go) **не хранит ID вообще** — нет ID-поля. То есть UUID, возвращаемый в `handleCreate`, не сохраняется. После рестарта демона все возвращённые ранее UUID становятся бесполезны.

**Fix**: добавить `ID string \`json:"id"\`` в `ExposedPort`, генерить UUID при создании и сохранять, искать по нему в DELETE.

### Q2 — **MEDIUM**: race condition на duplicate-subdomain check

**Где**: [`src/internal/daemon/api_ports.go:117-164`](src/internal/daemon/api_ports.go)

Сейчас:
```go
// L117: outside any lock
for _, ep := range cfg.ExposedPorts {
    if ep.HostHeader == req.Subdomain { ... 409 ... }
}
// ... 40 lines of work, including Caddy API call ...
// L162: enter write lock and append
h.cfgMgr.Update(func(c *config.RootConfig) {
    c.ExposedPorts = append(c.ExposedPorts, newPort)
})
```

Два concurrent POST с одинаковым subdomain могут оба пройти duplicate-check и оба добавить запись → дубль в config + два Caddy routes на один host (последний переопределяет первый, но первый "висит" зомби).

**Fix**: переместить duplicate-check **внутрь** `cfgMgr.Update` callback. ИЛИ использовать atomic compare-and-swap pattern.

### Q3 — **MEDIUM**: errors swallowed, request returns 201 при partial failure

**Где**: [`src/internal/daemon/api_ports.go:141-152`](src/internal/daemon/api_ports.go)

```go
if err := h.caddy.AddRoute(ctx, host, upstreamDial); err != nil {
    slog.Error("api: failed to add caddy route", "error", err)
    status = "error"   // ← marks status but DOESN'T return
}
// ... DNS call also swallowed ...
// ... 201 Created returned with status="error" ...
```

Запрос вернёт `201 Created` с `status: "error"` — но юзер ждёт 5xx. Это **misleading success response**. Caller думает что всё ок (HTTP 2xx), а потом ходит в logs понимать почему не работает.

**Fix**: при failure либо вернуть `502 bad_gateway` + откатить changes, либо документировать что 201+status=error — это "partial success" pattern (но тогда юзер должен явно читать body, что не очевидно).

### Q4 — **MEDIUM**: тесты покрывают ~30-40% production-critical путей

Тесты есть:
- ✅ `drift_test.go` (498 LOC, 10 тестов) — drift hashing, edge cases
- ✅ `persistence_test.go` (463 LOC, 7 тестов) — volume persistence, config restart
- ✅ `integration_test.go` (915 LOC, ~25 тестов) — compose gen, SSH validation, VPS config, port expose
- ✅ `config_test.go` (276 LOC, 10 тестов) — SecretString, atomic write, masking

Тестов **НЕТ**:
- ❌ `caddy_mtls.go BootstrapMTLS` — 381 LOC без unit-теста. Любой regression в этом коде = silent lockout.
- ❌ `tunnel/peer.go PeerManager` — 410 LOC. Peer-add flow с command-template injection — без теста.
- ❌ `tunnel/manager.go` — 530 LOC, 20+ Lock/Unlock — race-prone, без теста state machine.
- ❌ `tunnel/awg.go discoverWindows()` — platform-specific code, без теста.
- ❌ `proxy/caddy.go RemoveRoute` host-match — без теста на concurrent shift.
- ❌ Subdomain validation depth (FR-009 single-label) — нет теста на multi-level reject.

**Без unit-тестов на mTLS bootstrap и peer-add — silent regressions очень вероятны.**

### Q5 — **LOW**: `discoverWindows` идентичен `discoverDarwin`

[`src/internal/tunnel/awg.go:241-258`](src/internal/tunnel/awg.go) — функции просто скопированы. На Windows AmneziaWG TUN-адаптер именуется GUID'ом и его `awg show interfaces` может не возвращать его так как Linux. Тестов на реальную Windows-machine нет.

### Q6 — **LOW**: `newPort.External = 0` неиспользуется

[`src/internal/daemon/api_ports.go:157`](src/internal/daemon/api_ports.go): поле `External` в `ExposedPort` инициализируется в 0 с комментарием "assigned dynamically", но **никто его никогда не присваивает**. Либо удалить поле, либо документировать как "deprecated" / unused.

---

## Section 4 — ✅ Good (correctly implemented, well-designed)

Не всё плохо. Что сделано правильно:

1. ✅ **`SecretString` design** ([`config/secrets.go`](src/internal/config/secrets.go)) — separate `Plain()`, `RedactedString()`, `mask()`. Использован последовательно через все секретные поля (Password, PresharedKey, PrivateKey, ClientKey, Token, UIToken). Constitution Principle III satisfied.

2. ✅ **`GetMasked()` deep-copy + mask** ([`config/config.go:186-308`](src/internal/config/config.go)) — API-response masking реализована корректно через clone-then-mask, без мутации оригинала.

3. ✅ **Daemon binds `127.0.0.1`** ([`daemon/server.go:66-67`](src/internal/daemon/server.go)) — FR-005 satisfied. `addr := fmt.Sprintf("127.0.0.1:%d", s.port)`.

4. ✅ **awg-quick PATH check** ([`cmd/unet/startup.go:42-49`](src/cmd/unet/startup.go)) — FR-003a через `exec.LookPath`. Чистая diagnostic-ошибка если AmneziaWG не установлен.

5. ✅ **Single-instance lock** ([`cmd/unet/startup.go:54-101`](src/cmd/unet/startup.go)) — FR-007. Pidfile на POSIX, named-mutex на Windows. Stale-pidfile detection присутствует.

6. ✅ **`awg syncconf` через temp-file (без `<(...)`)** ([`tunnel/peer.go:249`](src/internal/tunnel/peer.go)) — antigravity F3 fix применён. **Но** см. RV3 — temp-path не уникальный.

7. ✅ **`encoding/json.MarshalIndent` для `clientsTable`** ([`tunnel/peer.go:311`](src/internal/tunnel/peer.go)) — antigravity F4 fix применён. JSON безопасен от injection через `clientName`.

---

## Section 5 — Prioritized Action Plan

### 🔴 P0 (must fix before merge to main)

1. **S1 — Rewrite `caddy_mtls.go::BootstrapMTLS`** to use SSH+`docker exec` injection pattern. Должен принимать `*ssh.Client` в конструкторе или через DI. (~120 LOC изменения в caddy_mtls.go + integration в ports/api lifecycle).
2. **RV1 — Pause-container в `compose.go`** — добавить `unet-net-pause` сервис, переместить `ports:` туда, оба amnezia и caddy используют `network_mode: "service:unet-net-pause"`. Обновить `data-model.md` §2.4 диаграмму и `research.md` §3.3 YAML.
3. **Q1 — Fix DELETE /api/ports/{id}**: добавить `ID` поле в `ExposedPort`, синхронизировать handleCreate/handleRemove. **Без этого DELETE сломан**.

### 🟠 P1 (fix before production deploy)

4. **RV2 — Local TCP-proxy** — реализовать listener на `tunnel.LocalIP:randPort` в `tunnel/manager.go` (или новый `tunnel/local_proxy.go`), `io.Copy` к `127.0.0.1:<userPort>`. Обновить `api_ports.go` чтоб использовать random tunnel-side port вместо user's local port. Обновить quickstart.md убрать `0.0.0.0` requirement.
5. **RV3 — `mktemp` + `flock`** в `peer.go::syncConf` и связанных функциях. Используя SSH execute pattern: `flock -w 10 /opt/amnezia/awg/awg0.lock -c '...'`.
6. **RV5 — Windows awg-quick reality** — либо переключиться на `golang.zx2c4.com/wireguard/windows` для Windows-ветки, либо документировать known limitation в quickstart.md (Windows: "manual setup of AmneziaWG via the official client; unet manages config only, not interface lifecycle").
7. **S2 — Single-label depth validation** — переписать subdomain validator с проверкой dns.mode.
8. **Q2 — Move dup-check inside Update lock**.
9. **Q3 — Caddy/DNS error handling** — либо rollback при partial failure либо явно документировать partial-success semantics.

### 🟡 P2 (technical debt; can land in follow-up PR)

10. **RV4 — CGNAT subnet** — поменять `awg_init.go::pickSubnet` на `100.64.X.0/24` (RFC 6598). Также обновить ВСЕ тестовые места в спеке где упоминается `10.8.1.x`.
11. **S3 — FR-010 sub-2 reconciliation** — `ReconcileStartupState()` в startup.
12. **Q4 — Unit tests для mTLS bootstrap, peer manager, tunnel manager, awg discoverer**. Minimum один happy-path + один failure-path per function. Целевое покрытие 60%+ для critical components.
13. **Q5 — Verify Windows discoverer** на реальной Windows-machine с AmneziaWG.
14. **Q6 — Удалить `External` поле или документировать as deprecated**.

---

## Verdict

```yaml
verdict: NEEDS-REWORK
reviewer: claude-code-review
reviewed_at: "2026-05-17T16:00:00Z"
critical_count: 2     # S1 (mTLS lockout regression), RV1 (Caddy lifecycle trap)
high_count: 4         # RV2, RV3, RV5, Q1
medium_count: 5       # RV4, S2, S3, Q2, Q3, Q4
low_count: 3          # Q5, Q6, (rest)
spec_drift_count: 3   # S1, S2, S3 (code не догнал post-Phase-4 spec)
review_md_addressed: 0/5 critical + 1/5 partial = 20%
implementation_lines_reviewed: 6251
test_lines_reviewed: 2152
recommendation: |
  Do NOT merge to main. Two CRITICAL findings (S1 mTLS lockout regression, RV1 netns lifecycle)
  contradict either the updated spec or fundamental Docker networking. Either:
  (a) Apply P0 fixes in this branch before merge, OR
  (b) Mark this PR as "scaffold/wip", merge to a long-lived dev branch, address P0-P1 in follow-ups.

  P0-P1 work estimated ~6-12 hours focused effort (~400-700 LOC code + tests).
```

---

## Recommendation для PR-флоу

Учитывая что:
- Implementation отстаёт от спеки (spec_drift на 3 fronts)
- 4/5 architectural findings из `.gemini/review.md` не адресованы
- 1 CRITICAL logic bug (broken DELETE)

Предлагаю:

**Option A (предпочительно)**: Применить P0 фиксы в этой же ветке (`001-init`), потом re-request review. Это держит линейную историю и `001-init` остаётся "первая полная реализация".

**Option B**: Закрыть PR #1, переименовать ветку в `001-init-scaffold`, открыть новые PR'ы (`002-fix-mtls-lockout`, `003-pause-container`, `004-local-tcp-proxy`, etc.) для каждого P0/P1 фикса по отдельности. Лучше для cherry-pick / revert, хуже для review-overhead.

**Option C**: Override gate через `/speckit.implement --override-gate "<reason>"` если есть deadline-давление. **Категорически НЕ рекомендую** — Principle VI override должен сопровождаться явным письменным rationale, и текущие CRITICAL слишком серьёзны (S1 — security lockout; RV1 — production crash trigger).
