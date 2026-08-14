# Backend

`project-framework` 的 Go HTTP 后端，使用 Gin 提供接口，并内置请求 ID、结构化访问日志、panic 恢复和优雅停机。当前公开能力是 `GET /healthz`。

自动化修改规则见 [AGENTS.md](AGENTS.md)。

## 运行

要求 Go 1.25+。在 `backend` 目录执行：

```bash
go run ./cmd/server
```

服务默认监听 `http://localhost:8080`。可通过以下命令验证：

```bash
curl --include http://localhost:8080/healthz
```

使用 `Ctrl+C` 或发送 `SIGTERM` 时，服务会在配置的超时时间内优雅停机。

## 配置

| 环境变量 | 默认值 | 约束与影响 |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | Go 监听地址，例如 `127.0.0.1:9000` |
| `SHUTDOWN_TIMEOUT` | `10s` | 必须是大于零的 Go duration |
| `GIN_MODE` | `release` | 未设置时使用 `release`；显式值保持不变 |

示例：

```bash
HTTP_ADDR=127.0.0.1:9000 SHUTDOWN_TIMEOUT=3s go run ./cmd/server
```

## HTTP 契约

### `GET /healthz`

服务可用时返回：

```http
HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8
X-Request-ID: <request-id>
```

```json
{"status":"ok"}
```

客户端传入 `X-Request-ID` 时保留原值；缺失时服务自动生成。未匹配路由、不支持的方法和未恢复的 handler panic 使用统一错误结构：

```json
{
  "error": {
    "code": "not_found",
    "message": "resource not found"
  },
  "request_id": "request-id"
}
```

## 架构边界

```text
cmd/server -> handler -> service -> infra
                    \-> model <-/
```

- `cmd/server`：唯一装配入口。
- `handler`：HTTP 协议、路由和 DTO。
- `service`：业务规则与副作用编排。
- `infra`：HTTP server 及真实外部依赖适配。
- `model`：跨 handler/service 的稳定业务类型。

## 验证

在仓库根目录执行：

```bash
make check-backend
```

修改 Go 文件后先运行 `gofmt`。API 契约变化还需执行根目录 `make check` 并完成前后端联调。
