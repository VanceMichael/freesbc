# 让 REGISTER 容量占用在成功应答前完成提交

> **Workspace:** /Users/vance/project/mineProject/swe/09101009/project-03

---

**User**

当绑定表只剩最后一个名额，而两个终端同时完成 REGISTER 时，客户端不应收到一个 FreeSBC 实际无法用于回呼的 200 OK。请把客户端注册的本地容量占用变成贯穿上游 REGISTER 事务的可提交预留：新绑定在转发前原子占位，刷新已有 (AoR, Call-ID) 在满表时仍可继续；只有 registrar 的 2xx 才按其授予的 expiry 提交绑定，并在提交成功后才向客户端恢复 Contact 和转发成功。challenge、非 2xx、传输错误、全池无响应、超时以及事务取消都要释放预留；同一来源的 TCP/TLS/WS/WSS 连接关闭时还要使未提交预留失效，防止迟到 2xx 安装已断开的绑定。容量拒绝返回稳定的 503 与 Retry-After，wildcard 或普通注销、过期清理和来源删除继续正确释放容量并触发现有订阅清理；容量释放后新的注册能够恢复成功，且失败计数和日志不得把被拒绝的本地提交记成已接受注册。

**Assistant**

我先了解一下工作区的代码结构，确认这是什么项目以及现有的 carrier/DNS/对话管理实现。

toolName: view_folder

status: success

/Users/vance/project/mineProject/swe/09101009/project-03

toolName: todo_write

status: success

Todos updated: 6 items

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/CLAUDE.md

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierdns.go

现在阅读 carrier 连接管理和转发路径相关文件。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierconn.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrier.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/forward.go

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierreg.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/dialog.go

现在阅读 in-dialog 请求路由、invite leg 以及 server 结构。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

继续阅读服务端装配、流连接跟踪与请求分发逻辑。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/edge.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/stream.go

阅读 INVITE leg 与路由记录建立的代码，以及 prack/update 入口。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/invite_leg.go

阅读 INVITE 主流程，确认入向 carrier 呼叫的建点与生命周期路径。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/invite.go

查看 PRACK/UPDATE 入口、subscribe/message 路径以及 metrics，确认所有向 carrier 侧发送的入口。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/prack.go

toolName: search_by_regex

status: success

查看现有 carrier 测试与测试夹具约定。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrier_stream_test.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierdns_test.go

toolName: search_by_regex

status: success

toolName: search_by_regex

status: success

查看 subscribe 路径确认 carrier 不会走订阅转发，并检查 design 文档的 carrier 章节。

toolName: search_by_regex

status: success

toolName: search_by_regex

status: success

调研完毕，开始实现。先更新任务状态，然后检查测试代码是否直接调用将改签名的内部函数。

toolName: todo_write

status: success

Todos updated: 6 items

toolName: search_by_regex

status: success

设计已定。开始实现——首先在 `carrierdns.go` 中加入创建时路由的代次注册表（retired pin registry）。

toolName: todo_write

status: success

Todos updated: 6 items

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierdns.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierdns.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierdns.go

changes: undefined

现在修改 `forward.go` 的 `requireFlow` 与 `writeRequest`，加入代次判定顺序。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/forward.go

changes: undefined

现在在 `carrierconn.go` 添加上下文 carrier 标识传递，并让 `carrierTarget` 复用快照方法。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierconn.go

changes: undefined

现在在 `dialog.go` 中把 pin 的释放绑定到对话唯一出口，并让 `usesRemote` 覆盖 early fork 路由。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/dialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/dialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/dialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/dialog.go

changes: undefined

现在给 `inviteAttempt` 和 `byeInfo` 加 carrier 归属字段。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/dialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/dialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/dialog.go

changes: undefined

现在在出站 carrier 呼叫路径接入 pin 与上下文。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrier.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrier.go

changes: undefined

现在在入向 carrier 呼叫路径（inviteToUpstream）钉住创建时路由。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/invite.go

changes: undefined

更新 `invite_leg.go`：CANCEL 与 teardown 发送携带创建时 carrier 身份。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/invite_leg.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/invite_leg.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/invite_leg.go

changes: undefined

现在更新 `indialog.go` 的所有发送点。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: failed

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

现在更新 onAck、onInDialog 的发送上下文与 BYE 补发，以及 sendMiddleBye。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

先修复测试目录构造器中 pins 初始化，然后格式化并编译。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierdns_test.go

changes: undefined

toolName: Shell

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

toolName: Shell

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

toolName: Shell

status: success

toolName: Shell

status: success

toolName: Shell

status: success

toolName: Shell

status: success

toolName: Shell

status: success

toolName: view_files

status: success

filePath: /var/folders/h2/dl6g5c0d2jjb29trgqldb6fw0000gn/T/trae-agent-toolhost-501/jobs/job-aa304c7f43164757bc209cb5dceaeb93/output.log

找到了：替换时少了一个闭合大括号（内层 switch 的）。修复并让 gofmt 规范化。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/indialog.go

changes: undefined

toolName: Shell

status: success

编译通过。运行 vet 与现有 carrier 相关测试确认无回归。

toolName: Shell

status: success

toolName: todo_write

status: success

Todos updated: 6 items

toolName: Shell

status: success

现有测试全部通过。查看测试夹具以便编写集成测试。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/harness_test.go

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/harness_test.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/harness_test.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrier_outbound_test.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/dialog_helpers_test.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/audit_lifecycle_test.go

查看 `client` 基座的 setAnswer 等辅助方法签名。

toolName: search_by_regex

status: success

现在编写单元测试文件。

toolName: Write

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierpins_test.go

changes: undefined

toolName: search_by_regex

status: success

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierpins_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierpins_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierpins_test.go

changes: undefined

toolName: Shell

status: success

单元测试通过。查看 carrierRig 与几个夹具辅助后编写集成测试。

toolName: search_by_regex

status: success

现在编写确定性集成测试（DNS/SRV 切换、新旧事务分流、断流重拨、CANCEL 原路、回收）。

toolName: Write

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carriergeneration_test.go

changes: undefined

清理集成测试里多余的 TLS 占位辅助。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carriergeneration_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carriergeneration_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carriergeneration_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carriergeneration_test.go

changes: undefined

toolName: Shell

status: success

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carriergeneration_test.go

changes: undefined

toolName: Shell

status: success

CANCEL 重拨成功，但退休路由的释放早于 edge 处理协程结束被检查。查看 `waitForReleaseEventually` 的判定。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/cleanup_test.go

`waitForReleaseEventually` 对 early 通话立即返回（只数 confirmed）。改为直接等待 pin 回收。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/call_test.go

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carriergeneration_test.go

changes: undefined

toolName: Shell

status: success

代次测试稳定。现在运行整个 edge 包的竞态测试套件。

toolName: todo_write

status: success

Todos updated: 7 items

toolName: Shell

status: success

edge 全部 race 通过。运行整个模块的测试与静态检查。

toolName: Shell

status: success

toolName: Shell

status: success

全部测试通过。现在同步更新文档中 carrier 连接与目录行为的描述。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/CLAUDE.md

changes: undefined

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/docs/design.md

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/docs/design.md

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/docs/design.md

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/docs/design.md

changes: undefined

文档已同步。现在对新增测试做 `-race -count` 稳定性验证，并校验示例配置。

toolName: Shell

status: success

toolName: Shell

status: success

全部通过。最后做一次完整代码走查，检查关键改动的最终形态。

toolName: Shell

status: success

toolName: Shell

status: success

toolName: view_files

status: success

filePath: /var/folders/h2/dl6g5c0d2jjb29trgqldb6fw0000gn/T/trae-agent-toolhost-501/jobs/job-f6c9a69849e140169e33a03fca1ba6a0/output.log

检查 docs/edge.md 是否有需要同步的已知限制描述。

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/docs/edge.md

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/docs/edge.md

changes: undefined

toolName: search_by_regex

status: success

`carrierTarget` 已无调用方，删除该死方法（语义由 `exactCarrier` 承担）。

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierconn.go

changes: undefined

toolName: Shell

status: success

最后再做一次全模块 `-race` 验证。

toolName: Shell

status: success

toolName: todo_write

status: success

Todos updated: 7 items

已完成。全模块 `go vet`、`gofmt`、`go test -race ./...` 全部通过。

## 问题根因

切换 A/AAAA 或 SRV 目标后，DNS 刷新发布新快照，已有通话的 BYE/PRACK/UPDATE 仍按对话创建时记录的旧远端发送，但 [requireFlow](file:///Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/forward.go) 在连接池未命中时只在**当前快照**里查找 carrier（旧地址已不在），找不到就返回 `errNoFlow`——旧连接一旦断开，存量通话即无法沿原路重拨。

## 实现方案（代次归属 = 快照 + 对话引脚）

**1. 创建时路由注册表**（[carrierdns.go](file:///Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierdns.go)）
- 新增 `carrierPin` 引用计数注册表（`retain/pinnedRoute/retiredOwner/pinCount`），键为 `(carrier, transport, dest)`，与快照完全隔离：退休地址永不回流到快照地址集、`carrierFor`、`isSource` 或任何新事务选址。
- 快照新增 `exactCarrier`：精确地址匹配，只服务**新出对话事务**。刷新失败仍保留 last-good（不发生退休）；地址再变回当前代时由快照正常选取。

**2. 对话生命周期绑定**（[dialog.go](file:///Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/dialog.go)）
- `dialog.end()` 的唯一状态迁移是唯一释放点：CANCEL、非 2xx、超时、媒体故障、BYE、关机、并发结束都恰好释放一次；最后一个相关对话结束即回收退休路由。
- `attachRoutePin` 处理 end 竞态（迟到引脚立即释放）。
- 出站 carrier 呼叫在首个 INVITE 前引脚（[carrier.go](file:///Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrier.go)）；入站呼叫仅在源是其精确解析地址时引脚（临时端口不引脚，[invite.go](file:///Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/invite.go)）。
- `usesRemote` 扩展到 early fork 路由，振铃期连接也不会被空闲超时关闭。

**3. 拨号三级归属顺序**（[forward.go](file:///Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/forward.go#L329-L352)、[carrierconn.go](file:///Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierconn.go#L140-L171)）

池内活连接先复用；未命中时：① ctx 携带的对话引脚 carrier（确定性归属，跨 carrier 复用同一旧地址也不会借错 TLS/SNI/证书）→ ② 当前快照（新事务）→ ③ 唯一退休归属（代理自发 ACK/BYE 的无提示兜底）；两个 carrier 共享同一退休地址则歧义即失败关闭。ACK/BYE/PRACK/UPDATE/INFO/NOTIFY、CANCEL（`inviteAttempt.carrier`）、`ack2xx/ackThenBye/sendMiddleBye`（`byeInfo.carrierName`）全部携带该提示。

**4. 兼容性**：UDP 引脚只是账本，不产生任何连接；字面 IP/单地址 carrier 的代次永不变化；拓扑隐藏、错误码、指标标签零改动。

**5. 确定性测试**
- [carrierpins_test.go](file:///Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carrierpins_test.go)：引脚幂等/回收、刷新后快照与 admission 隔离、跨 carrier 同址歧义、并发引用、early fork 占用、迟到引脚释放。
- [carriergeneration_test.go](file:///Users/vance/project/mineProject/swe/09101009/project-03/internal/edge/carriergeneration_test.go)（真实回环，SRV 切换目标端口）：新呼叫走新代次而旧呼叫旧连接复用；旧流断开后旧对话的 BYE 重拨旧远端（tcp 与 tls 两个子用例，验证重拨沿用同一 carrier 的 SNI/CA）；early 阶段 CANCEL 重拨旧远端并回收引脚。
- 文档同步：[CLAUDE.md](file:///Users/vance/project/mineProject/swe/09101009/project-03/CLAUDE.md)、[design.md §6.4/6.7/6.10](file:///Users/vance/project/mineProject/swe/09101009/project-03/docs/design.md)、[edge.md](file:///Users/vance/project/mineProject/swe/09101009/project-03/docs/edge.md)。
