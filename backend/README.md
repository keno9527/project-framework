# Backend

`project-framework` 的 Go HTTP 后端，使用 Gin 提供工作流发现、草稿校验与执行接口，并内置请求 ID、结构化访问日志、panic 恢复和优雅停机。

自动化修改规则见 [AGENTS.md](AGENTS.md)。

## 运行

要求 Go 1.25+。在 `backend` 目录执行：

```bash
go run ./cmd/server
```

启动时会从工作流目录严格加载并编译全部 YAML，任一文件无效则服务不启动。服务默认监听 `http://localhost:8080`。可通过以下命令验证：

```bash
curl --include http://localhost:8080/healthz
```

使用 `Ctrl+C` 或发送 `SIGTERM` 时，服务会停止接受新运行，并在配置的超时时间内等待真实节点退出后优雅停机。

## 配置

| 环境变量 | 默认值 | 约束与影响 |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | Go 监听地址，例如 `127.0.0.1:9000` |
| `SHUTDOWN_TIMEOUT` | `10s` | 必须是大于零的 Go duration |
| `GIN_MODE` | `release` | 未设置时使用 `release`；显式值保持不变 |
| `WORKFLOW_DIR` | `./workflows` | 工作流 YAML 目录，可使用相对或绝对路径 |
| `MAX_ACTIVE_RUNS` | `8` | 同时接受的运行数，正整数 |
| `MAX_CONCURRENT_NODES` | `16` | 全局实际执行节点数，正整数 |
| `MAX_NODES_PER_RUN` | `4` | 单流程并行节点数，正整数 |
| `RUN_TIMEOUT` | `2m` | 整体运行超时（含调度与重试），大于零的 Go duration |

示例：

```bash
HTTP_ADDR=127.0.0.1:9000 WORKFLOW_DIR=/tmp/workflows RUN_TIMEOUT=90s go run ./cmd/server
```

## HTTP 契约

成功响应为 `{"data": ...}`，错误响应为 `{"error":{"code","message"},"request_id":"..."}`。完整契约见 [api/openapi.yaml](api/openapi.yaml)。

| 方法 / 路径 | 用途 |
| --- | --- |
| `GET /healthz` | 存活检查 |
| `GET /readyz` | 就绪检查，返回 `instanceId` |
| `GET /api/v1/node-types` | 已注册节点类型与数据契约 |
| `GET /api/v1/workflows` | 已加载流程和版本 |
| `GET /api/v1/workflows/{workflowId}/versions/{version}` | 已加载版本的编译图和节点描述快照（敏感字段遮盖） |
| `POST /api/v1/workflows/validate` | 提交 `{definition}`，返回编译后的 `view` 和规范化 `yaml` |
| `POST /api/v1/test-runs` | 提交 `{definition,input}`，校验并执行草稿，返回 `202` |
| `POST /api/v1/runs` | 执行已加载的精确版本，返回 `202` |
| `GET /api/v1/runs/{runId}` | 查看运行状态、尝试记录与成功输出 |

### `GET /healthz`

```json
{"status":"ok"}
```

客户端传入 `X-Request-ID` 时保留原值；缺失时服务自动生成。草稿校验失败返回 `422 DEFINITION_INVALID`，输入与 Schema 不符返回 `422 INPUT_INVALID`，容量不足返回 `429 CAPACITY_EXCEEDED`。

运行记录仅保留在当前进程；重启或淘汰后查询返回 `404 RUN_NOT_FOUND`。日志只记录运行标识、状态、耗时与错误类别，不记录业务输入或完整输出。

## 架构边界

```text
cmd/server -> handler -> service -> infra
                    \-> model <-/
```

- `cmd/server`：唯一装配入口。
- `handler`：HTTP 协议、路由、请求体限制和错误码到状态码映射。
- `service/workflow`：工作流用例门面，负责解析、遮盖和错误语义。
- `internal/workflow`：纯领域引擎（节点契约、Schema、注册表、YAML 加载、DAG 编译与执行、内存记录）。
- `internal/nodes`：文本与订单排查样例节点的显式注册。
- `internal/jsonvalue`：保留数字精度的 JSON 解析。
- `infra`：HTTP server 及真实外部依赖适配。
- `model`：跨 handler/service 的稳定业务类型。

## 验证

在仓库根目录执行：

```bash
make check-backend
```

也可在 `backend` 目录运行真实 HTTP 冒烟（需先启动服务）：

```bash
python3 scripts/smoke.py
```

修改 Go 文件后先运行 `gofmt`。API 契约变化还需执行根目录 `make check` 并完成前后端联调。
