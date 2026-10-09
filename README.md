# Project Framework

一个前后端分离的 Go 工作流编排工程：用 Go 实现并注册节点，用 YAML 定义 DAG，通过 Web 控制台查看和编辑流程草稿、测试运行，并通过 HTTP 执行已加载版本。后端使用 Gin，前端使用 React、TypeScript 与 Vite。

## 当前能力

| 模块 | 入口 | 能力 |
| --- | --- | --- |
| 后端 | `backend/cmd/server/main.go` | 工作流加载编译、节点目录、草稿校验、DAG 执行、运行记录、请求 ID、结构化日志、异常恢复和优雅停机 |
| 前端 | `frontend/src/main.tsx` | 流程列表、节点目录、React Flow 画布、草稿编辑与测试运行面板 |
| 验证 | 根目录 `Makefile` | 统一执行后端测试与前端完整检查 |

## 快速开始

环境要求：Go 1.25+、Node.js `^22.13.0` 或 `>=24.0.0`、npm。

先启动后端（默认加载 `backend/workflows` 下的样例）：

```bash
cd backend
go run ./cmd/server
```

再在另一个终端启动前端：

```bash
cd frontend
npm install
npm run dev
```

打开 Vite 输出的本地地址。开发服务器会把 `/healthz`、`/readyz` 和 `/api` 代理到 `http://localhost:8080`。也可以直接检查后端：

```bash
curl http://localhost:8080/healthz
```

预期响应为 `{"status":"ok"}`。

## 工程边界

```text
project-framework/
├── backend/          # Go HTTP API 与工作流引擎
│   ├── api/          # OpenAPI 契约
│   ├── internal/
│   │   ├── handler/  # Gin 路由、DTO 与错误映射
│   │   ├── service/  # 用例门面
│   │   ├── workflow/ # 纯领域引擎（加载、编译、执行）
│   │   ├── nodes/    # 样例节点注册
│   │   └── jsonvalue/# 精确数字解析
│   └── workflows/    # 工作流 YAML
├── frontend/         # React Web 应用
│   └── src/
│       ├── api/      # 后端请求与契约类型
│       └── pages/    # 控制台页面
├── AGENTS.md         # 跨目录开发规则
├── Makefile          # 统一验证入口
└── README.md         # 项目入口
```

后端以 `cmd/server` 为唯一装配入口，运行时调用保持 `handler -> service -> infra`，领域逻辑位于 `internal/workflow`；前端页面通过 `src/api` 访问后端。

## 详细文档

| 内容 | 开发说明 | 自动化修改规则 |
| --- | --- | --- |
| 后端运行、配置与 HTTP 契约 | [backend/README.md](backend/README.md) | [backend/AGENTS.md](backend/AGENTS.md) |
| 前端命令、API 配置与联调 | [frontend/README.md](frontend/README.md) | [frontend/AGENTS.md](frontend/AGENTS.md) |
| 跨端规则与完成标准 | 本文件 | [AGENTS.md](AGENTS.md) |

## 质量检查

在仓库根目录执行全部检查：

```bash
make check
```

也可以按范围执行：

```bash
make check-backend
make check-frontend
```
