# Frontend

`project-framework` 的 React、TypeScript 与 Vite 前端，提供工作流控制台：查看已加载流程与节点目录，编辑流程草稿、校验并执行测试运行。画布基于 React Flow 与 Dagre。

自动化修改规则见 [AGENTS.md](AGENTS.md)。

## 快速开始

要求 Node.js `^22.13.0` 或 `>=24.0.0` 和 npm。本地联调前先确保后端监听 `http://localhost:8080` 并已加载工作流。

```bash
npm install
npm run dev
```

Vite 会输出本地访问地址，并把 `/healthz`、`/readyz` 和 `/api` 代理到后端。

## 常用命令

| 命令 | 用途 |
| --- | --- |
| `npm run dev` | 启动开发服务器 |
| `npm run build` | 执行 TypeScript 检查并生成生产构建 |
| `npm run preview` | 本地预览生产构建 |
| `npm run lint` | 执行 ESLint |
| `npm test` | 运行全部 Vitest 测试 |
| `npm run test:watch` | 监听模式运行测试 |
| `npm run check` | 依次执行 lint、测试和构建 |

## 调用链

```text
src/main.tsx
  -> src/App.tsx
    -> src/pages/workflow          # 控制台外壳、画布、草稿编辑与测试运行
      -> src/api/workflow.ts        # 请求、错误转换、轮询
        -> src/api/workflow.types.ts# 契约类型
          -> /api/v1/...
```

控制台视图与草稿逻辑内聚在 `src/pages/workflow`，`src/api/workflow.ts` 负责请求、错误转换、响应运行时校验与轮询。

## API 配置

本地开发时，`vite.config.ts` 将 `/healthz`、`/readyz` 和 `/api` 代理到 `http://localhost:8080`。

生产环境默认使用同源路径。如需请求不同源 API，复制 `.env.example` 到对应的 Vite 环境文件：

```dotenv
VITE_API_BASE_URL=https://api.example.com
```

值末尾无需添加 `/`。跨域部署还需要 API 或网关允许前端来源。

## 验证

在仓库根目录执行：

```bash
make check-frontend
```

该命令执行 ESLint、全部 Vitest 测试、TypeScript 检查和生产构建。

## 常见问题

| 现象 | 检查方式 |
| --- | --- |
| 页面提示无法连接服务 | 确认后端已启动，并运行 `curl http://localhost:8080/readyz` |
| 修改后端端口 | 同步调整 `vite.config.ts` 中的代理目标 |
| 跨域请求失败 | 检查 `VITE_API_BASE_URL` 和 API 的 CORS 配置 |
| 安装或构建失败 | 运行 `node --version`，确认满足 `package.json` 的 `engines` |
