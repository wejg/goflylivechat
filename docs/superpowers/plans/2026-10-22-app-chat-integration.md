# App embedded chat implementation plan

> Execution: superpowers:subagent-driven-development. Approved by the user in this chat; edit the two existing checkouts directly, without commits or deployment.

Goal: App-authenticated, one-use ticket entry into GoFly chat with separate PostgreSQL/MySQL storage and account/session isolation.
Architecture: go_home authenticates users and stores 60-second ticket hashes tied to auth_sessions. GoFly redeems tickets over an authenticated server request and issues a revocable, HttpOnly cookie; every visitor operation and WebSocket checks that identity. Existing anonymous demo mode remains only when integration is explicitly disabled.
Spec: the detailed proposal immediately preceding the user's implementation authorization in this chat.
Constraints: no App frontend changes; no new runtime dependency; no shared database, raw App tokens, secrets in URLs, unsolicited commits, production changes or account merging. Source namespace comes from server configuration, never client headers.

## Contract
- App POST /api/support/chat-session uses existing access-token, returns {code:"ok",data:{url:"https://chat/livechat#ticket=...",expires_in:60}}.
- GoFly POST /visitor/session accepts JSON {ticket:string}; calls App POST /api/internal/support/tickets/consume.
- Internal requests use header X-Support-Key and JSON. Both internal endpoints always return App envelope {code:"ok",data:...} on success.
- consume request {ticket:string}; response data {app_id:string,user_id:string,source_session_id:string,source_expires_at:RFC3339,name:string,avatar:string,extra:map[string]string}.
- POST /api/internal/support/sessions/check request {app_id:string,user_id:string,source_session_id:string}; success data {active:bool,expires_at:RFC3339}. Invalid/revoked/deleted sessions return active:false, not user details. Key failure 401; invalid payload 400; DB failure 503/500.
- App configuration SUPPORT_CHAT_ENABLED, SUPPORT_CHAT_URL (fixed /livechat URL), SUPPORT_CHAT_APP_ID, SUPPORT_CHAT_INTEGRATION_KEY. HTTPS mandatory except explicit APP_ENV=development.
- GoFly configuration GOFLY_APP_CHAT_ENABLED, GOFLY_APP_API_BASE_URL, GOFLY_APP_ID, GOFLY_APP_INTEGRATION_KEY; HTTPS except explicit GOFLY_ENV=development. Cookie Secure default true, development may use false explicitly.
- GoFly app sessions: opaque random cookie, hash at rest, 2-hour absolute max capped at source expiry, validity confirmation <=60s. Expired confirmation with failed source validation fails closed; active WebSockets independently enforce deadlines. Cookie write operations verify exact same-origin/CSRF. App mode rejects legacy visitor JWT/login. Agent JWT stays supported for agent operations.

## Tasks
- [x] 1. App backend: failing security tests; ticket migration/store and existing-auth session helpers; public issuance and authenticated consume/check routes; validated config/wiring; isolated PostgreSQL integration tests incl replay/concurrency/revocation.
- [x] 2. GoFly backend: failing auth tests; source API client, visitor mapping and DB session migration, cookie exchange/logout, shared middleware and WS validation, expiry/revocation; TLS fake App and MySQL integration tests.
- [x] 3. GoFly UI and resources: ticket fragment exchange and stripping, no identity/localStorage fallback in App mode; unified visitor sending auth, protected attachment uploads/downloads and static-path guard; template/route browser proof.
- [x] 4. Docs/config and cross-service validation: examples without real secrets; repeatable setup and run commands; isolated DBs and full two-service login/history/send/WS/attachment tests; security review and relevant regression suites.

## Ownership and review
Task 1 owns /Users/wei/go_home/backend/app only. Task 2 owns GoFly common/app_chat.go, models/chat_session.go, controller/app_session.go, middleware/app_visitor.go, middleware/visitor_scope.go, ws/visitor.go plus new focused tests. Primary owns router, cmd, templates, controller/message.go/visitor.go, attachment models/controllers, docs and integration tests. Avoid edits to the same file concurrently; request shared changes from primary. All tasks self-review; independent final security review after integration.

## Progress
- Preflight: both workspaces clean. Ruling: modify existing user-requested checkouts rather than worktrees; preserve current identifiers and agent routes. Mode defaults disabled so unconfigured deployments keep functioning; enabling App mode closes legacy identity paths.

## Verification (2026-10-22)
- Both repositories compile all Go packages; both Compose configurations validate; both git diff checks pass.
- App focused auth/config/support/httpapi/migrate tests passed against isolated PostgreSQL 18.6. Focused auth/support/httpapi race tests passed. Existing legal-page branding assertion remains unchanged and excluded from the focused run.
- GoFly full ordinary suite passed against isolated MariaDB 10.11 with only legacy external-side-effect TestSendSmtp and TestImportSql skipped. Focused TestApp/TestPrivate race tests passed. Full controller race testing still hits the inherited unread fixture replacing the global DB while a legacy auto-reply goroutine runs; no claim of full-suite race cleanliness.
- Built both actual services, with separate seeded PostgreSQL and MariaDB instances. Chromium browser (Browser plugin not available, installed Playwright fallback) passed real App entry -> one-use redemption -> HttpOnly cookie -> visitor UI send -> agent WS reply -> private upload/download -> same-page account switch -> old credential rejection -> App source-session revocation and composer disable. Verified ticket stripped, no App token in chat, replay/legacy identity/cross-account requests denied, runtime script errors 0.
- Desktop 1167x810 and mobile 390x844 rendered screenshots inspected. Fixed composer sizing with a shrinking message list; editor remains within viewport. Same-document ticket hash navigation now reloads to exchange the new identity. Screenshots: /tmp/gofly-app-chat-e2e-desktop.png and /tmp/gofly-app-chat-e2e-mobile.png.
- Independent security review found no remaining blocking issue after strong JWT startup validation, cookie rotation failure compensation, agent metadata ownership and normalized attachment reference fixes.
- No Flutter changes, deployment, production schema migration, commit or push. Existing localhost:8081 service and database left unchanged. Disposable test servers and databases cleaned up after verification.
