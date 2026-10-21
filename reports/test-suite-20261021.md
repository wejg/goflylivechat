# 六项测试结果（2026-10-21）

本轮测试全部使用独立 Docker 容器和临时 MariaDB。现有 `goflylivechat-app-1`、`goflylivechat-db-1` 没有被重启或写入测试数据。

| 项目 | 结果 | 证据 |
| --- | --- | --- |
| 1. 响应中断 / 重试 | **复现风险** | 第一次请求 HTTP 响应被代理丢弃，客户端收到 `UND_ERR_SOCKET`，数据库已有 1 条；等待限流窗口后盲目重试，数据库变成 2 条。说明当前接口需要幂等键或客户端确认机制。 [JSON](/Users/diannao/goflylivechat/reports/loadtest-20261021122432247-ada49c/result.json) |
| 2. 限流边界 | **通过** | 同一 IP 500ms 内被拒绝，约 4 秒后成功；业务限流逻辑符合当前实现。 [JSON](/Users/diannao/goflylivechat/reports/loadtest-20261021122645498-0b05d1/result.json) |
| 3. 长时间稳定性 | **未通过** | 100 访客＋5 客服持续 900 秒，105 条 WebSocket 始终在线；消息逐条入库和投递全通过，但 10 次 `message_status` 出现 `ECONNRESET`。应用内存峰值约 246 MiB，数据库约 288 MiB，`Slow_queries=0`、`OOMKilled=false`。 [JSON](/Users/diannao/goflylivechat/reports/loadtest-20261021133414175-d352c0/result.json) |
| 4. 断线 / 重启恢复 | **100 规模通过，高规模失败** | 100 规模的重连、重新登录、应用重启、未读恢复均通过；300 和 500 在恢复洪峰中出现连接重置，1000 出现大量恢复请求失败。 |
| 5. 逐步升压 | **拐点明确** | 100 持续阶段通过；300 持续阶段通过但恢复洪峰失败；500 持续阶段通过，应用内存峰值约 511.9 MiB；1000 持续阶段失败，应用达到 512 MiB 限额并有大量 `ECONNRESET`。 [300 JSON](/Users/diannao/goflylivechat/reports/loadtest-20261021132818550-ec6857/result.json) · [500 JSON](/Users/diannao/goflylivechat/reports/loadtest-20261021132922844-56896b/result.json) · [1000 JSON](/Users/diannao/goflylivechat/reports/loadtest-20261021133033199-4674ed/result.json) |
| 6. 真实浏览器流程 | **通过基础流程** | Chrome 本地浏览器登录客服页面成功；首页→设置→聊天切换后聊天 iframe 仍在；在线访客↔最近访客切换正常；聊天输入框和发送按钮可见。当前线上页面没有专门注入的测试访客，因此没有在真实浏览器中伪造一条消息来验证未读徽标。 |

响应中断测试把真实的失败窗口做出来了：服务端先执行数据库写入，再返回 HTTP 响应；客户端连接在响应阶段被断开时，服务端没有办法知道客户端是否收到结果。当前相同内容再次提交会重复保存 2 条。此问题不能靠前端“失败就重试”解决，需要幂等消息 ID、服务端去重，或客户端在重试前查询发送结果。

升压测试使用了合法且唯一的模拟 IP；连接建立支持批量模式，避免把一次性握手洪峰和持续连接能力混在一起。1000 规模仍然达到应用 512 MiB 限额，不能作为可支持 1000 访客的结论。300、500 的持续阶段消息核对通过，失败集中在恢复时同时发起大量 HTTP 请求；这是恢复洪峰的容量问题，不能写成普通持续负载失败。

浏览器测试通过本机 `http://localhost:8081` 完成。`127.0.0.1` 页面因允许来源配置不同首次出现“连接失败”，改用 `localhost` 后登录和页面流程正常；这也说明生产部署时允许来源配置需要覆盖实际访问域名。

最后验证：`go test ./...` 全部通过；`node --test tests/*.test.js` 62 个用例全部通过；`node --check tests/load/run.mjs` 和 `git diff --check` 通过。

本次业务源码保留的最小修复是 [ws/visitor.go](/Users/diannao/goflylivechat/ws/visitor.go:23) 的访客连接关闭；压测脚本增加了响应中断、限流边界、批量连接、逐条请求错误和合法 IP 生成。现有运行中的业务容器没有重建，修复需要后续部署才能作用于线上。

详细根因说明见 [loadtest-rootcause-20261021.md](/Users/diannao/goflylivechat/reports/loadtest-rootcause-20261021.md)。
