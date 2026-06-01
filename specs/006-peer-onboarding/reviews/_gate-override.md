# Gate Override: 006-peer-onboarding

**Date**: 2026-06-01
**Override by**: Undre (repo owner)
**Reason**: Gemini review returned CRITICAL verdict (F1: nip.io private IP routing). F1 is accepted as a real bug — will be fixed during implementation by using VPS public IP instead of WG client IP in nip.io subdomain format. F2 (bootstrap context cancellation) accepted — will use detached context. F3 (short-code per-code attempt tracking) partially accepted — primary defense is per-IP rate limit. F4 (QR size) accepted — will default to 512x512. F5 (code entropy) noted for future improvement. No second external reviewer available — override with owner approval.

**Findings disposition**:
- F1 (CRITICAL): ACCEPTED — nip.io subdomain format changed from `<label>.<wg-client-ip-dashed>.nip.io` to `<label>.<vps-public-ip-dashed>.nip.io`. Fix applied in TASK-3.3 and TASK-6.1 during implementation.
- F2 (HIGH): ACCEPTED — bootstrap runs in detached context (`context.WithoutCancel` or `context.Background`). HTTP handler returns immediately, progress via SSE. Fix applied in TASK-6.2.
- F3 (HIGH): PARTIALLY ACCEPTED — per-IP rate limit is primary defense. Per-code tracking kept for matched hashes only. Fix applied in TASK-5.4.
- F4 (MEDIUM): ACCEPTED — QR default size increased to 512x512. Fix applied in TASK-5.1.
- F5 (LOW): NOTED — alphanumeric short codes considered for v0.2. Current 8-digit numeric sufficient for v0.1 with rate limiting.

**Verdict**: OVERRIDE — proceeding to implementation with F1-F4 fixes incorporated.
