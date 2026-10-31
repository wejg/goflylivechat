# Support Chat UX Implementation Plan

**Goal:** Implement the visitor chat improvements approved in the conversation: truthful service status, helpful guidance, reliable retry, attachment confirmation, and compact readable messages.

**Architecture:** Keep the existing Vue 2 page, shared chat-window component and authenticated Go endpoints. Keep server persistence and idempotency authoritative; do not invent read receipts or response-time guarantees.

**Spec:** User approved all recommendations in this conversation on 2026-11-01.

**Constraints:** Keep dark/light theme and existing brand accent, no new dependencies, daily Beijing service hours 10:00–18:00 (an actually connected agent remains online outside these hours), maximum 1000 UTF-16 input units (within the server's 2048 Unicode-character limit), preserve session security and draft text.

## Tasks

- [x] Add regression tests for separate guidance, short service status, question prompts, retained failed bubbles, stable retry keys, input length, attachment preview/confirmation/progress/failure and draft preservation.
- [x] Implement visitor page logic and markup in `static/templates/chat_page.html`: static heading, state-aware guide outside history, compact question shortcuts, 1000-character composer, per-message send/retry state, independent attachment preview/upload/send lifecycle, and accessible image viewing.
- [x] Adjust `static/css/visitor-chat.css`: preserve theme tokens, compact timestamps and grouped messages, neutral automatic reminders, clear progress and retry controls, responsive preview dialog and composer. Keep targets at least 44px.
- [x] Update `static/js/chat-window.js` to retain failed local messages within its bounded history window, use explicit history labels and preserve the existing reading anchor/new-message navigation behavior. Update its tests.
- [x] Verify backend supports 1000-character content and existing idempotent retry contract; add focused API coverage where possible without changing its public limits or authentication.
- [x] Run affected Node tests, `go test ./...`, `git diff --check`, then browser-check mobile light/dark layout, service states, keyboard/composer, shortcuts, failed sends and attachment confirmation with deterministic local fixtures.

## Verification ledger

- Baseline: previous turn identified an unrelated failure in `tests/main_unread.test.js` (`request.success is not a function`); reproduce if it remains.
- Deployment is outside this change; report browser fixture checks separately from real server/database and real-device checks.

## Completed verification

- Independent final review: no remaining actionable findings. It independently reran the 85 visitor/shared-list tests and diff checks.
- `go test ./...`: passed; MySQL-dependent integration cases skipped because no isolated database was configured.
- `node --test tests/*.test.js`: 113 passed, one pre-existing logout mock failure (`tests/main_unread.test.js:42`), reproduced on the unchanged baseline in the earlier task.
- Mobile Chrome browser fixture: passed dark/light layout, 390px and 320px widths, reduced 340px viewport, 960-character input, question shortcuts, separate draft/attachment handling, explicit attachment confirmation, image viewer focus/Escape, historical image viewing, same-key failed-message retry, service heartbeat transitions and saved-confirmation timing. APIs were mocked; these are UI checks, not live database or actual iOS keyboard checks.
- Native frontend `/Users/wei/go_home/frontend`: neutral support title updated in Chinese, traditional Chinese and English; WebView theme supplies new accent-foreground/error variables. Formatting and analysis passed; five support page/WebView tests passed. The broader navigation suite has eight push-startup failures, independently reproduced on its unchanged HEAD `f47fb17`.
- Browser screenshots: `/Users/wei/.codex/visualizations/2026/10/31/01a193ea-f781-77e3-9149-c0375f56b655/support-ux/`.
- No deployment, commit, native release build, or physical-device verification performed.

## Online status follow-up

Fixed scheduled hours masking actual agent presence. Raw connected status now takes priority in the visitor status, guide, composer and saved-message hint; only offline agents use the scheduled-hours fallback. Offline automatic reminders now also stop whenever the assigned agent is connected. Reproduced the old behavior with two failing frontend cases before fixing it. All 85 targeted visitor/shared-list tests, the local real-WebSocket heartbeat check, full Go tests and mocked-API mobile browser regression passed. The new MySQL integration case is present but skipped without an isolated database. No deployment performed.

## Media loading follow-up

Reproduced the outgoing image flash in mobile Chrome by blocking the newly uploaded remote image: its bubble initially measured 34×41px before expanding. Sent images now retain the already loaded local blob preview with explicit dimensions; server messages still use the uploaded path and existing idempotency. Failed sends keep the preview; evicted messages and page teardown release blob URLs, with open image viewers retaining their URL until closed. File cards reserve their full footprint while icons load. The same blocked-image regression now shows the decoded local image immediately at stable 160×160px with no remote-image fetch. A blocked PDF icon test shows the full filename/card at stable 236×66px, including 320px viewport coverage. All 86 targeted Node tests and the broader mocked-API mobile browser flow passed. No deployment or real-device validation performed.
