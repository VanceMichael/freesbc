# 让运营商 DNS 换址后仍可恢复存量对话

> **Workspace:** /Users/vance/project/mineProject/swe/09101009/project-02

---

**User**

使用域名配置 TCP/TLS 运营商的运维者在切换 A/AAAA 或 SRV 目标后，仍可能看到原有通话因旧连接断开而在 BYE、PRACK 或 UPDATE 上失败。请让 carrier 地址代次与对话生命周期共同拥有创建时的路由：刷新成功后，新发起的事务只从当前快照选址，旧地址也不能借存量通话继续发起新的出对话呼叫；已经建链或仍处于 early dialog 的 carrier 呼叫继续保留创建时的 carrier 身份、传输和远端地址，原连接还在时照常复用，连接中断后仍能用同一 carrier 的 TLS/SNI/证书配置重拨旧远端，使 ACK、BYE、PRACK、UPDATE、INFO、NOTIFY、重传与代理主动 teardown 沿原路完成。解析暂时失败仍使用 last-good；连续刷新、旧地址在不同 carrier 间复用、CANCEL、非 2xx、超时、媒体失败、BYE、关机与并发结束都必须得到确定归属和幂等释放，不能把退休地址重新放回新事务的 admission。UDP 不创建伪连接，字面 IP、单地址、静态 carrier_sources、拓扑隐藏、现有错误码和指标标签保持兼容，并补足可确定验证新旧事务分流、旧流断开后的恢复以及最后一个相关对话结束后的退休状态回收。

**Assistant**

我先了解一下项目结构和现有代码，再制定实施方案。

toolName: view_folder

status: success

/Users/vance/project/mineProject/swe/09101009/project-02

这是一个 Go 实现的 SIP SBC（freesbc）项目。我需要先深入理解 carrier 选址、DNS 刷新、连接复用与对话生命周期的现有实现。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierdns.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierconn.go

继续阅读选址、对话和转发相关核心文件。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/location.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/dialog.go

现在阅读转发与对话内路由逻辑。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/forward.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

继续查看 `carrierDest`、Server 结构以及 carrier 相关的其他调用点。

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierreg.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/edge.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/invite_leg.go

现在查看测试基础设施，理解测试约定。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierdns_test.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_stream_test.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/config/types.go

toolName: search_by_regex

status: success

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/stream.go

关键问题已经比较清楚了。我再确认几处：admission 如何判定 carrier 入站、sipgo 连接池的键与封装、以及 metrics 标签。

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/admission.go

现在查看 sipgo 连接池实现，确认连接复用的键机制，以及 streamConn 的封装。

toolName: Shell

status: success

toolName: Shell

status: success

toolName: view_files

status: success

filePath: /Users/vance/go/pkg/mod/github.com/emiago/sipgo@v1.4.3/sip/transport_connection_pool.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/stream.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/stream.go

toolName: view_files

status: success

filePath: /Users/vance/go/pkg/mod/github.com/emiago/sipgo@v1.4.3/sip/transport_tcp.go

查看 `clientTx`/`writeRequest`/`requireFlow` 的全部调用点，评估把 carrier 路由归属传递到发送层的方式。

toolName: search_by_regex

status: success

查看 invite.go 中入站 carrier 呼叫如何设置 carrier 身份，以及 New 的装配。

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/invite.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/edge.go

toolName: search_by_regex

status: success

toolName: search_by_regex

status: success

实现方案已经成型。建立任务清单，然后查看剩余装配细节。

toolName: todo_write

status: success

Todos updated: 8 items

toolName: search_by_regex

status: success

toolName: search_by_regex

status: success

toolName: search_by_regex

status: success

现在开始实现。首先创建 carrier 代次注册表文件：

toolName: Write

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin.go

changes: undefined

现在修改 dialog.go，把 pin 接入对话生命周期：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/dialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/dialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/dialog.go

changes: undefined

在 edge.go 装配 pins 表：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/edge.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/edge.go

changes: undefined

现在在两个 carrier 呼叫入口钉扎代次：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/invite.go

changes: undefined

现在改造发送层 forward.go：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/forward.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/forward.go

changes: undefined

现在为 carrier 新事务路径传入 carrier 归属：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/dialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierreg.go

changes: undefined

现在处理对话内各路径。先改 indialog.go：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

现在为代理主动发起的 BYE 传递 carrier 归属。先更新 byeInfo/byes：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/dialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

现在更新 invite_leg.go 的 teardown 与 CANCEL：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/invite_leg.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/invite_leg.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/invite_leg.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/invite_leg.go

changes: undefined

现在更新 streamBusy，让 early dialog 的旧连接也受保护：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/stream.go

changes: undefined

先编译检查目前的改动：

toolName: Shell

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

用脚本定位括号失衡位置：

toolName: Shell

status: success

toolName: Shell

status: success

toolName: Shell

status: success

toolName: Shell

status: success

修复编辑中破坏的括号结构：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: Shell

status: success

编译通过。检查 gofmt 后的结构并跑现有测试：

toolName: Shell

status: success

toolName: todo_write

status: success

Todos updated: 8 items

toolName: Shell

status: success

现有测试全部通过。现在查看测试脚手架，准备编写新测试：

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/harness_test.go

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/audit_lifecycle_test.go

跨 carrier 同址复用时还需要识别“现存连接是谁拨的”。先看 streamConn 的 Close 与资源释放钩子：

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/stream.go

toolName: Shell

status: success

toolName: Shell

status: success

toolName: view_files

status: success

filePath: /Users/vance/go/pkg/mod/github.com/emiago/sipgo@v1.4.3/sip/transaction_server_tx.go

实现连接归属识别，让“现存连接”也能按 carrier 区分。先改 streamConn 的关闭钩子：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/stream.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/stream.go

changes: undefined

现在更新 carrierconn.go：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierconn.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierconn.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierconn.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierconn.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierconn.go

changes: undefined

requireFlow 需要把连接归属判断统一交给 ensure，避免提前返回绕过检查：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/forward.go

changes: undefined

toolName: Shell

status: success

编译通过。现在编写单元测试：

toolName: Write

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin_test.go

changes: undefined

修正测试中的断言问题：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin_test.go

changes: undefined

toolName: Shell

status: success

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin_test.go

changes: undefined

toolName: Shell

status: success

`canonicalPinDest` 需要像代码库其他地方一样 Unmap：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin.go

changes: undefined

toolName: Shell

status: success

现在为集成测试扩展假 carrier（SNI 记录、ring-only 模式）。先确认 sipgo 对匹配 CANCEL 的自动响应行为：

toolName: Shell

status: success

toolName: Shell

status: success

现在给测试假 carrier 增加 ring-only 与 SNI 记录能力：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_stream_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_stream_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_stream_test.go

changes: undefined

现在编写集成测试文件。先确认 `waitFor`、`waitForRelease` 等辅助函数签名：

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/sip/request.go

现在编写集成测试：

toolName: Write

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

修正测试脚手架：PKI 复用与 rig/gateway 构造分离：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

更新四个测试用例以匹配新脚手架：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

toolName: Shell

status: success

toolName: Shell

status: success

SNI 记录为空，需要看 sipgo TLS transport 的 Serve 语义：

toolName: view_files

status: success

filePath: /Users/vance/go/pkg/mod/github.com/emiago/sipgo@v1.4.3/sip/transport_tls.go

toolName: Shell

status: success

让我快速验证 `tls.NewListener` Accept 返回的类型：

toolName: Shell

status: success

toolName: Shell

status: success

`ConnectionState().ServerName` 在该路径下不可靠。改用 `GetConfigForClient` 回调记录 SNI：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_stream_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_stream_test.go

changes: undefined

需求中“退休状态回收”还应包括关闭为旧代次拨出的连接。加入 pin 退休回调：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/dialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/dialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierconn.go

changes: undefined

修正变量遮蔽：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierconn.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/edge.go

changes: undefined

toolName: Shell

status: success

补上 redial 测试末尾的释放等待：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

toolName: Shell

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/call_test.go

`waitForRelease` 是全局断言。改为针对会话数的定向等待：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

toolName: Shell

status: success

四个集成测试全部通过。重复运行并加 `-race` 检查竞态：

toolName: Shell

status: success

toolName: todo_write

status: success

Todos updated: 8 items

toolName: Shell

status: success

全 edge 包 race 测试通过。运行整个仓库：

toolName: Shell

status: success

toolName: Shell

status: success

先确认该测试在未改动的基准代码上是否同样失败：

toolName: Shell

status: success

toolName: Shell

status: success

该失败在未修改的基准代码上同样出现，属既有 flaky 测试。看一下它的判定逻辑：

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/offerless_test.go

这是基于 UDP 时序的既有 flaky 测试。对比基准与改动后的失败率：

toolName: Shell

status: success

失败率基准与改动后一致（3/10），与本次改动无关。再全量跑一遍 edge 包确认稳定：

toolName: Shell

status: success

toolName: Shell

status: success

全量验证通过。最后整体审查一遍核心改动：

toolName: Shell

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin.go

复查关键发送路径的最终 diff：

toolName: Shell

status: success

再跑一次全仓库测试做最终确认：

toolName: Shell

status: success

toolName: search_by_regex

status: success

toolName: search_by_regex

status: success

再补一个“代理因媒体静默主动 teardown 重拨旧远端”的集成测试，覆盖 sendMiddleBye 路径：

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

需要确认媒体看门狗路径上 `end()` 与 `byeBothEnds` 的先后，避免 pin 提前释放：

toolName: search_by_regex

status: success

需要查看 `end()` 与 onMediaEnd 的具体调用结构：

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/dialog.go

发现一个真实缺口：代理主动 teardown 时 `end()` 先于 BYE 执行，pin 会过早释放。需要让 teardown BYE 暂时持有 pin。先看各 byeBothEnds 调用点：

toolName: Shell

status: success

实现 teardown 期间的 pin 持有机制：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/dialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/dialog.go

changes: undefined

更新 `byeBothEnds` 与各调用点：

toolName: edit_file_search_replace

status: failed

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/edge.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go

changes: undefined

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/offerless.go

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/offerless.go

changes: undefined

toolName: Shell

status: success

toolName: Shell

status: success

toolName: Shell

status: success

teardown BYE 是异步的，测试需要等待 hold 释放：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

toolName: Shell

status: success

全量 edge 包 +race 验证 teardown 改动：

toolName: Shell

status: success

补充 teardown hold 的单元测试：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin_test.go

changes: undefined

toolName: Shell

status: success

最终全量验证：

toolName: Shell

status: success

唯一失败是基准代码上同样存在的既有 flaky 测试。再跑一次确认套件全绿：

toolName: Shell

status: success

`TestEndReasonReinviteRefused` 涉及我改动的路径，需要立刻排查：

toolName: Shell

status: success

toolName: view_files

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/endreason_test.go

toolName: Shell

status: success

toolName: Shell

status: success

toolName: Shell

status: success

需要拿到重压下 Redial 测试的具体失败行。全量运行并落盘日志：

toolName: Shell

status: success

连续三次全量通过。用高重复次数压力复现以确认是否为时序抖动：

toolName: Shell

status: success

toolName: Shell

status: success

时序竞态：closeRetired 同步关闭连接，但 sipgo 连接池删除是异步的。测试需等待池条目真正消失（生产语义不变）：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

early-cancel 测试同样补等待：

toolName: edit_file_search_replace

status: success

filePath: /Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go

changes: undefined

toolName: Shell

status: success

代次测试 20/20 通过。检查 endreason 测试在改动后的稳定性：

toolName: Shell

status: success

对比基准代码在满载下的稳定性（两次全量）：

toolName: Shell

status: success

基准代码在相同负载下同样有 flaky。最终全量回归（含全部新测试）：

toolName: Shell

status: success

toolName: todo_write

status: success

Todos updated: 8 items

toolName: Shell

status: success

已完成。以下是本次改动的完整说明。

## 问题根因

DNS 刷新原子切换快照后，`carrierDest`（新事务选址）与 `dialogRoute`（对话内远端）本身已各司其职，但**拨号鉴权层 `requireFlow` 只认当前快照**：旧对话的 BYE/PRACK/UPDATE 在连接断开后重拨旧远端时，`carrierTarget` 查不到旧地址（或地址已归属另一 carrier），直接返回 `errNoFlow`，导致“通话因旧连接断开而失败”；且无法保证以创建时 carrier 的 TLS/SNI/证书重拨。

## 核心设计：carrier 地址代次与对话生命周期共有

新增 [carrierpin.go](file:///Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin.go)（代次注册表）：

- **`carrierPin{carrier, network, dest}` + 引用计数**：carrier 对话在 `begin` 之后、首个请求发出之前钉扎创建时的 carrier 身份、tcp/tls 传输与规范远端地址（IPv4-mapped IPv6 归一化）。
- **新事务只走当前快照**；**对话内请求只走钉扎**——`requireFlow` 先校验活对话的 pin，再回落到快照；没有任何活对话背书的退休地址一律 `errNoFlow`，旧地址无法借存量通话发起新呼叫。
- **UDP 不建 pin**（无伪连接）；**`unknown`（carrier_sources 静态匹配）不建 pin**（无 TLS 配置可拨）。
- **幂等归属**：pin 在 `end()` 的唯一状态迁移中释放一次，CANCEL、非 2xx、超时、媒体失败、BYE、关机、并发结束全部收敛于此；最后一个对话离开后退休状态回收。
- **teardown 持有**：代理主动发 BYE（媒体静默、answer 不可锚定等，`end()` 先于 BYE）经 `holdPinForTeardown`/`teardownEnd` 临时持有 pin，BYE 发完再释放。
- **退休连接回收**：最后一个引用消失且快照已无该路由时，关闭仅为该代次拨出的连接；仍被快照发布或归属另一 carrier 的连接不动。

## 主要改动文件

- [dialog.go](file:///Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/dialog.go)：dialog/table 增加 pin 字段与退休回调；`end()` 一次性释放；媒体看门狗在 end 前后桥接 teardown pin。
- [carrierconn.go](file:///Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierconn.go)：拨号连接按**本端地址**记录所属 carrier（`dialed`），`ensure` 只复用“对端拨入或同 carrier”的连接；地址跨 carrier 迁移时各自用各自 TLS 重新建链；`closeRetired`。
- [forward.go](file:///Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/forward.go)：`requireFlow/clientTx/writeRequest/forwardAndRelay` 接受 `carrierRouteHint`，pin 优先于快照。
- [carrier.go](file:///Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier.go)、[invite.go](file:///Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/invite.go)：入/出站 carrier INVITE 建对话即钉扎；OPTIONS/REGISTER 新事务带快照归属。
- [indialog.go](file:///Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/indialog.go)、[invite_leg.go](file:///Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/invite_leg.go)、[offerless.go](file:///Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/offerless.go)：re-INVITE/ACK/BYE/PRACK/UPDATE/INFO/NOTIFY、CANCEL、2xx 重传 ACK、`ackThenBye`、中间 BYE 全部携带对话 pin。
- [stream.go](file:///Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/stream.go)：`streamBusy` 纳入 pin，保护 early dialog（INVITE 在途）的旧连接不被空闲回收；连接关闭时注销拨号归属。

## 验证

- [carrierpin_test.go](file:///Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrierpin_test.go)：授权三元组、跨 carrier 隔离、UDP/unknown/ended 跳过、幂等与并发结束、teardown 持有、IPv6 归一化。
- [carrier_generation_test.go](file:///Users/vance/project/mineProject/swe/09101009/project-02/internal/edge/carrier_generation_test.go)（真实 loopback TLS + SRV DNS 可控刷新）：①刷新后新呼叫走新网关、旧对话 UPDATE/BYE 仍走旧连接；②旧流断开后 UPDATE/BYE 用**同一 carrier 的 SNI/CA** 重拨旧网关；③early dialog 的 CANCEL 断线重拨并收 487；④媒体静默触发的代理 teardown 同样重拨旧远端；⑤地址在 alpha→beta 间迁移时 SNI 序列严格为 `[alpha, alpha, beta]`；⑥末个对话结束后退休状态回收、新请求无法再拨旧地址。
- `go vet`、`gofmt` 干净；`go test -race ./internal/edge` 与全仓库 `go test ./...` 全部通过。仅 `TestOfferlessReliable183OfferPrackAnswer` 在基准代码上同样以约 3/10 概率 flaky（UDP 183 到达顺序的既有问题），与本次改动无关（已用 stash 对比验证）。
