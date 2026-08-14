# Backend Agent Guide

本文件适用于 `backend/`。根目录 `AGENTS.md` 的共同规则继续生效；发生冲突时，以本文件为准。

## 启动路径

1. 阅读 `backend/README.md`、`go.mod` 和目标包的现有测试。
2. 从 `cmd/server/main.go` 确认依赖装配，从 `internal/handler/router.go` 确认公开路由。
3. 沿 `handler -> service -> infra` 追踪 DTO 转换、业务状态、副作用和错误映射。
4. 先运行目标包测试确认基线；行为变更先写最小失败测试。

## 包边界

| 路径 | 职责 | 不应包含 |
| --- | --- | --- |
| `cmd/server` | 配置加载、依赖组装、监听器和信号处理 | 业务规则、HTTP DTO、数据访问 |
| `internal/config` | 环境变量、默认值和校验 | 业务状态、外部 client 初始化 |
| `internal/handler` | Gin 路由、中间件、协议 DTO 和 HTTP 错误映射 | 数据库/RPC 调用、跨资源业务规则 |
| `internal/service/<domain>` | 领域用例、状态流转、幂等和副作用编排 | Gin、HTTP 状态码、外部 SDK 结构 |
| `internal/model` | 跨 handler/service 的稳定领域类型 | 请求 DTO、ORM 对象、通用杂项函数 |
| `internal/infra/<adapter>` | HTTP server、数据库、RPC 或消息适配器 | handler、业务编排、公开 DTO |

`cmd/server` 是唯一装配入口。只有真实外部依赖出现时才新增 adapter；只有依赖确实变化或存在多个真实实现时才抽取接口。

## API 与运行时

- 请求体严格解码和校验；handler 定义公开 DTO，service 返回业务错误语义，handler 映射 HTTP 状态码。
- 错误响应保持 `{ "error": { "code", "message" }, "request_id" }`，不得泄漏堆栈、SQL、路径、密钥或内部异常。
- handler 将 `c.Request.Context()` 传入 service，并保留请求 ID、访问日志和 panic recovery。
- 日志使用稳定字段，不记录完整请求体、token 或个人敏感数据；新增副作用必须明确幂等与失败处理。
- HTTP server 保留读取超时和按配置执行的优雅停机语义。

## 变更联动

| 场景 | 必须核对 |
| --- | --- |
| 新增 HTTP 接口 | handler 路由、DTO、错误映射、契约测试及对应 service |
| 新增业务状态 | model 定义、service 校验、handler 映射和边界测试 |
| 新增环境变量 | `config.Config`、默认值/合法值/非法值测试和 `backend/README.md` |
| 接入外部依赖 | service 窄接口、infra 实现、`cmd/server` 注入及失败路径 |
| 修改 API 契约 | 按根目录 `AGENTS.md` 同步前端和公开文档 |

## 验证

修改 Go 文件后运行 `gofmt`。先执行目标包测试，完成前在仓库根目录执行 `make check-backend`；跨端契约变化按根目录规则执行 `make check` 和本地联调。
