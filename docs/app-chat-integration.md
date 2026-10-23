# App 内嵌客服接入

两套数据库继续独立：App 后端从 PostgreSQL 验证现有登录身份，GoFly 向 App 后端兑换一次性票据，在 MySQL 保存访客映射和聊天会话。唯一用户标识是服务端配置的 `app_id` 加 App 数据库 `users.id`；GoFly 自己生成稳定的 `visitor_id`。同账号换设备保留历史，不同账号不会合并。

## 已实现的入口

App 点击客服时先请求 App 后端，不再直接打开 `/livechat?user_id=agent`：

```http
POST /api/support/chat-session
access-token: <当前 App 登录凭证>
Content-Type: application/json

{"source":"settings"}
```

成功返回：

```json
{"code":"ok","data":{"url":"https://chat.example.com/livechat#ticket=<一次性票据>","expires_in":60}}
```

WebView 顶层打开返回的 `url`。网页立即移除地址中的票据并兑换 HttpOnly Cookie，HTTP 和 WebSocket 共用这一身份；App Token 不传给网页或 GoFly。票据只可使用一次、最多有效 60 秒；失败或过期后重新请求入口，不能重复使用原地址。

订单入口可提交 `{"source":"order_detail","order_id":"<订单列表的公开订单编号>"}`，App 后端验证订单属于当前用户。来源允许 `app/settings/order_detail/membership/feedback/help`。不能传 `user_id`、设备 ID、邮箱或客服账号来选择用户身份。客服侧显示账号编号、昵称、会员状态及有限的来源/客户端资料；两端不复制整张用户表。

## 两端配置

默认关闭，已有匿名网站接入保持原行为。启用后，旧访客自报身份登录、旧访客 JWT 入口均被拒绝。

App 后端环境变量（也已在 `/Users/wei/go_home/backend/docker-compose.yml` 透传）：

```dotenv
SUPPORT_CHAT_ENABLED=true
SUPPORT_CHAT_URL=https://chat.example.com/livechat
SUPPORT_CHAT_APP_ID=go_home_prod
SUPPORT_CHAT_INTEGRATION_KEY=<随机服务密钥A>
```

GoFly 环境变量（Compose 已透传）：

```dotenv
GOFLY_APP_CHAT_ENABLED=true
GOFLY_ENV=production
GOFLY_APP_API_BASE_URL=https://api.example.com
GOFLY_APP_PUBLIC_ORIGIN=https://chat.example.com
GOFLY_ALLOWED_ORIGINS=https://chat.example.com
GOFLY_APP_ID=go_home_prod
GOFLY_APP_INTEGRATION_KEY=<同一个随机服务密钥A>
GOFLY_APP_DEFAULT_AGENT=agent
GOFLY_JWT_SECRET=<另一个随机密钥B>
GOFLY_SESSION_SECRET=<另一个随机密钥C>
```

`GOFLY_APP_API_BASE_URL` 是 App API 的固定 origin，不包含 `/api`；后端自动追加内部接口路径。`GOFLY_APP_PUBLIC_ORIGIN` 必须与浏览器地址栏完全相同，包括端口，不带末尾斜杠；反代后也填写外部 HTTPS origin。`GOFLY_ALLOWED_ORIGINS` 同时包含该 origin，否则 WebSocket 会被拒绝。默认客服账号必须存在。两端 `app_id`、服务密钥必须一致；密钥 B/C 不与 A 共用。

每个密钥单独运行 `openssl rand -hex 32` 生成，不要使用示例占位值，也不要把密钥放入客户端、网页配置或 Git。开启 App 模式时 GoFly 拒绝缺失/弱 JWT 密钥；更换 JWT 密钥会使旧客服登录 Token 失效，客服需要重新登录。生产要求 HTTPS；仅本地开发可设置 App 的 `APP_ENV=development` 和 GoFly 的 `GOFLY_ENV=development` 使用 HTTP。

局域网联调要把客户端打开的聊天 URL 配置为电脑的局域网 IP（例如 `http://192.168.1.20:8081/livechat`），并同步配置 public origin/allowed origins；手机的 `localhost` 指手机自己。两个容器不在同一网络时，后端内部请求使用双方实际可达地址，不能使用另一个容器的 `localhost`。

## 数据库和上线顺序

App 新增 `000030_support_chat` 迁移，保存票据哈希和来源会话。先保持接入关闭，按 App 现有 expand/contract 部署流程完成迁移，再开启；已有 contract 27/29 未完成时不能越过它们直接启用 30。详细命令及边界见 [App 后端接入文档](/Users/wei/go_home/backend/app/docs/support-chat.md)。

GoFly 开启模式后启动时检查/创建 `app_visitor_mapping`、`visitor_session`、`private_upload` 表，并升级访客昵称列以保存 emoji。失败则拒绝启动，不带着缺失的权限表提供服务。不会删除历史聊天记录或合并旧匿名访客。部署前备份两端数据库，准备真实域名、TLS 和独立强密钥，更新两端程序与配置后再切 App 入口。

附件改由受保护的 `/attachments/:id` 下载，拥有者和当前分配客服可访问；禁止直接访问 `/static/upload`，防止绕过权限。**开启模式后，旧历史中的公开 upload 链接会无法打开**，旧上传头像需重新上传保存；新头像经显式保存发布到 `/static/avatars`。Compose 已持久化头像目录，用户不要把聊天附件手工复制到公开目录。

## 会话失效和 App 客户端

客服 Cookie 最长 2 小时，且不能超过来源 App 会话到期。GoFly 至多每 60 秒重新向 App 确认来源会话；来源撤销、禁用或无法确认时拒绝请求并关闭 WebSocket。签发新入口时撤销同一浏览器的旧客服 Cookie，不影响同账号其他设备。

App 客户端还需完成（本次没有修改 Flutter）：

1. 点击客服调用签发接口并打开返回 URL；不手拼用户资料、不注入 App Token。
2. 退出或切换账号时，先在旧页面同源调用 `DELETE /visitor/session`，关闭旧 WebView/连接并清理对应 Cookie；不能让旧页面继续显示上一账号历史。
3. 网页会发送 DOM 事件 `support-session-expired`；若原生提供 `GoFlySupport.postMessage`，会收到 `{"type":"support_session_expired"}`。通知桥只允许可信客服域名，收到通知后重新获取入口；退出流程也必须自行关闭旧页面，不依赖通知。
4. 使用顶层 WebView，避免跨站 iframe Cookie 限制；完成 iOS/Android 真机的上传、附件、键盘、前后台恢复和返回验收。

内部接口为 `POST /api/internal/support/tickets/consume` 和 `POST /api/internal/support/sessions/check`，专用 `X-Support-Key` 请求头，只在服务器间使用。故障不降级为匿名身份。内部接口仍可由网络/反代进一步限制到 GoFly 来源，不能让 CDN 缓存会话、历史或附件。

## 本地验证

测试只能指向一次性数据库，不能使用业务库。GoFly 用 `GOFLY_TEST_DB=1` 和 `GOFLY_DB_*`，App 用 `TEST_DATABASE_URL`。已覆盖票据并发消费/重放、来源撤销、跨账号/跨来源、切号撤销、WS 失效、客服归属和附件权限。两端完整 Go 包编译，以及两个真实服务和独立 PostgreSQL/MySQL 的浏览器联调结果记录在实现计划中。

GoFly 遗留 packr/envy 依赖会在工作目录存在 `.env` 时覆盖同名进程环境变量；直接运行本地二进制时应让该文件与测试环境一致，或从没有 `.env` 的隔离目录运行。Docker 最终镜像不复制 `.env`，使用 Compose 注入变量。

2026-10-22 验证结果：两端全部 Go 包编译通过；App 认证/客服/配置/迁移相关测试及定向 race 测试通过；GoFly 普通套件通过（跳过会对外发信/导入业务库的两个旧测试），App/附件定向 race 测试通过。两个真实服务使用独立 PostgreSQL 18.6 / MariaDB 10.11，Chromium 浏览器验证签发、兑换、消息发送、客服 WS 回复、附件隔离、同页切号撤销、App 来源撤销全部通过，脚本错误 0；桌面和手机尺寸输入框完整可见。

App 原有协议页品牌文案断言仍失败，本次未改协议；GoFly 全量 race 有旧 unread 测试切换全局 DB 与自动回复协程冲突，不能声称全量 race 无问题。未运行真实 Safari/iOS/Android WebView、生产迁移或部署。
