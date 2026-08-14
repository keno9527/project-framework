# Project Framework

一个可直接扩展的前后端分离工程骨架：Go 与 Gin 提供 HTTP API，React、TypeScript 与 Vite 提供页面。当前通过 `GET /healthz` 展示前后端连接状态。

## 当前能力

| 模块 | 入口 | 能力 |
| --- | --- | --- |
| 后端 | `backend/cmd/server/main.go` | 健康检查、请求 ID、结构化日志、异常恢复和优雅停机 |
| 前端 | `frontend/src/main.tsx` | 展示后端检查中、连接成功和不可用状态 |
| 验证 | 根目录 `Makefile` | 统一执行后端测试与前端完整检查 |

## 快速开始

环境要求：Go 1.25+、Node.js `^22.13.0` 或 `>=24.0.0`、npm。

先启动后端：

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

打开 Vite 输出的本地地址。开发服务器会把 `/healthz` 代理到 `http://localhost:8080`；也可以直接检查后端：

```bash
curl http://localhost:8080/healthz
```

预期响应为 `{"status":"ok"}`。

## 工程边界

```text
project-framework/
├── backend/          # Go HTTP API
├── frontend/         # React Web 应用
├── AGENTS.md         # 跨目录开发规则
├── Makefile          # 统一验证入口
└── README.md         # 项目入口
```

后端以 `cmd/server` 为唯一装配入口，运行时调用保持 `handler -> service -> infra`；前端页面通过 `src/api` 访问后端。没有真实需求时，不预先增加数据库、RPC、全局状态或通用抽象层。

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
