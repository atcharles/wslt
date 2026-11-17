# wslt
websocket

### go get -u github.com/atcharles/wslt

### 服务端广播与关闭

现有导出 API 保持兼容，客户端代码和协议不变。`SendToAll` 成功仅表示消息已进入广播队列，
不保证每个连接已收到。广播分发采用非阻塞入队，单个连接队列满时关闭并注销该慢连接；
业务如需可靠投递，应另有确认、重试或状态补拉机制。

```go
ws := wslt.NewWSocket(
    wslt.WithBroadcastQueueSize(1024),
    wslt.WithConnectionQueueSize(256),
    wslt.WithBroadcastTimeout(2*time.Second),
)
defer ws.Close()

ctx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()
err := ws.SendToAllContext(ctx, "lotteryDraw", data)
```

- 默认广播队列容量为 **1024 条**，每连接发送队列为 **256 条**。
- `SendToAll` 默认最多等待队列空位 **2 秒**，超时返回 `context.DeadlineExceeded`。
- `SendToAllContext(ctx context.Context, msgType string, data interface{}) error` 使用调用方的取消/期限，
  返回 `ctx.Err()`；未设置期限的 Context 可以一直等待，建议业务显式设置超时。
- 三个新增 `With...` 选项通过现有 `WsOptionFunc` 配置，仅在构造时使用；非正参数忽略。
- `Connection.SendWsMessage` 保持原有最多 **100ms** 的队列等待；广播内部不等待单连接队列。
- `ConnectionCount() int` 返回当前已注册连接数。
- `Close()` 幂等关闭 hub 和连接，同时释放等待广播入队的调用；待发消息不保证排空。

连接只关闭 `closeChan`，不关闭 `sent`，避免并发发送与关闭通道的竞态；注销通知在连接锁外
非阻塞执行，hub 忙碌或退出时直接从受锁保护的连接表移除。遍历使用连接快照，回调在锁外执行。
读期限和 pong handler 由读泵初始化，ping 由写泵直接写 socket，沿用 **5 秒**写期限，符合
[Gorilla 的单读者、单写者约束](https://pkg.go.dev/github.com/gorilla/websocket#hdr-Concurrency)。

期限约束适用于队列等待，不能中断业务数据的同步序列化。用户提供的 `IterationConnections`
回调仍在 hub 循环内执行，应快速返回，避免阻塞后续注册、遍历和广播。并发关闭与发送重叠时，
可能已经成功入队但随后因关闭而不再投递；成功入队不构成送达确认。

`server_broadcast_test.go` 覆盖 1000 次广播/发送/关闭交错、满发送/广播队列、取消及 hub 退出、
慢连接隔离、满队列下 ping、注册前关闭，以及 httptest 本地收发与断开注销；维护责任在服务端
连接及 hub 生命周期代码，客户端回归沿用 `client_close_test.go`。
