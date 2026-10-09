# 合并 workflow-diy 到 project-framework 实施计划

## 调研结论

- **workflow-diy**：Go 工作流编排系统。
  - `workflow/`：纯领域引擎（节点契约、JSON Schema 校验子集、注册表、YAML 加载、DAG 编译、并发执行引擎、内存运行记录），仅依赖标准库 + `gopkg.in/yaml.v3`。
  - `internal/jsonvalue`：保留数字精度的 JSON 解析；`internal/nodes`：文本与订单排查样例节点的显式注册。
  - `internal/api`：基于标准库 `net/http`（Go 1.22 ServeMux）的 HTTP 层。
  - `web/`：React 19 + TypeScript + React Flow(`@xyflow/react`) + Dagre 的控制台，含流程列表、节点目录、草稿编辑、校验与测试运行；草稿存浏览器，35 个单测。
  - `api/openapi.yaml` 契约；`workflows/*.yaml` 两个样例；`scripts/smoke.py` 真实 HTTP 冒烟。
- **project-framework**：Gin v1.12 骨架，严格 `cmd/server → handler → service → infra`；环境变量配置；前端 `main → App → pages/<feature> → api → backend`，`@` 别名指向 `src`。
- **关键约束确认**：Gin v1.12 已支持同层静态段与参数段共存（`/workflows/validate` 与 `/workflows/:workflowId/...` 可同时注册）。
- **语义要点**：草稿/试运行需读取原始 body 并经 `jsonvalue` 解析，以保留数字精度、拒绝未知字段与 `[REDACTED]` 占位，故沿用「读原始字节 → 服务端解析」路径，而非常规 Gin bind。

## 文件与模块

### 后端 `backend/`

- `internal/workflow/`：**新增**，由 `workflow-diy/workflow/*.go`（含测试）整体复制；改写内部导入 `…/internal/jsonvalue` → `project-framework/internal/jsonvalue`。纯领域包，不引入 Gin。
- `internal/jsonvalue/`：**新增**，复制 `workflow-diy/internal/jsonvalue/`（含测试）。
- `internal/nodes/`：**新增**，复制 `workflow-diy/internal/nodes/`（含测试）；`workflow` 导入改写为 `project-framework/internal/workflow`。
- `workflows/text-demo.yaml`、`workflows/order-investigation.yaml`：**新增**样例配置。
- `api/openapi.yaml`：**新增**契约文件。
- `scripts/smoke.py`：**新增**，按新目录适配的冒烟脚本。
- `internal/config/config.go`、`config_test.go`：**修改**，新增 `WorkflowDir` 与运行上限/超时的环境变量配置、默认值与校验。
- `internal/service/workflow/`：**新增**用例门面 `service.go`（节点目录、流程列表、版本视图、草稿校验、试运行、已加载版本执行、运行查询、就绪与关闭），移植原 `internal/api` 的解析/错误语义；`service_test.go`。
- `internal/handler/workflow.go`：**新增** Gin DTO、8 个端点与错误码→HTTP 映射；读取并限制原始 body。
- `internal/handler/router.go`、`router_test.go`：**修改**，注入 workflow service 并注册新路由与 `/readyz`；更新现有测试。
- `cmd/server/main.go`：**修改**，组装 registry→nodes.Register→LoadDir→Freeze→engine→service，服务停止后 `engine.Shutdown`。
- `go.mod`/`go.sum`：`go mod tidy` 增加 `gopkg.in/yaml.v3`。

### 前端 `frontend/`

- `src/api/workflow.types.ts`：**新增**，源自 `web/src/types.ts` 的契约类型。
- `src/api/workflow.ts`：**新增**，源自 `api.ts`；含 `APIError`、请求/POST 助手、`useAPI`；统一加 `VITE_API_BASE_URL` 前缀。
- `src/pages/workflow/`：**新增**控制台功能目录：
  - `index.tsx`（源 `App.tsx` 的控制台外壳，导出 `WorkflowConsole`）；
  - `DraftWorkspace.tsx`、`NodeEditor.tsx`、`RunPanel.tsx`、`WorkflowCanvas.tsx`、`WorkflowDetails.tsx`、`components.tsx`、`graph.ts`、`draft.ts`；
  - `workflow.css`（源 `styles.css` 控制台样式）、`run-panel.css`；
  - 测试 `index.test.tsx`、`DraftWorkspace.test.tsx`、`RunPanel.test.tsx`、`draft.test.ts`、`graph.test.ts`；`test/fixtures.ts`。
  - 导入改写：`types`→`@/api/workflow.types`，`api`→`@/api/workflow`，同目录组件保持 `./`。
- `src/App.tsx`：**修改**为渲染 `WorkflowConsole`。
- `src/main.tsx`：**修改**，引入 `@xyflow/react/dist/style.css`。
- `package.json`：**新增**依赖 `@xyflow/react@12.11.6`、`@dagrejs/dagre@3.1.1`。
- `vite.config.ts`：**修改**，代理 `/api`、`/readyz`（保留 `/healthz`）到 `http://localhost:8080`。
- `src/styles.css`：精简为基础 reset/令牌（控制台样式就近放于 pages/workflow）。
- **删除**不再被引用的演示：`src/pages/home/`、`src/api/health.ts`、`src/api/health.test.ts`（后端 `/healthz` 保留）。

## 实施步骤（依赖顺序）

1. 后端：复制 `jsonvalue`、`workflow`、`nodes` 与样例 YAML、OpenAPI、smoke 脚本，批量改写导入路径。
2. 后端：扩展 `config` 与测试。
3. 后端：新增 `service/workflow`，移植解析与错误语义（含草稿/试运行/运行）。
4. 后端：新增 handler 端点与错误映射，改造 router 并更新其测试。
5. 后端：改造 `cmd/server/main.go` 装配与 engine 生命周期；`go mod tidy`。
6. 后端：运行 `gofmt`、`go test ./...`、`go vet`。
7. 前端：加入两个依赖并安装；新增 `api/workflow(.types)`。
8. 前端：建立 `pages/workflow` 全部组件、样式与测试，改写导入。
9. 前端：改造 `App.tsx`、`main.tsx`、`vite.config.ts`，精简全局样式，删除 home 演示。
10. 前端：运行 `npm run check`。
11. 全量：仓库根 `make check`；本地起前后端联调并执行 `scripts/smoke.py`。

## 依赖与注意事项

- 新增 Go 依赖 `gopkg.in/yaml.v3`（引擎专用），由 `go mod tidy` 引入。
- 前端 Node 版本需满足目标 `^22.13.0 || >=24.0.0`；新增两个运行时依赖。
- 契约路径、字段、状态码、错误码与 workflow-diy 完全保持一致（成功 `{"data":...}`，错误 `{"error":{code,message}}`）。
- 日志只记运行标识/状态/耗时/错误类别，不记业务输入输出；视图对敏感字段名递归遮盖。

## 验证

- `cd backend && gofmt -l ./... && go test ./... && go vet ./...`
- `cd frontend && npm run check`（lint + 单测 + 生产构建）
- 根目录 `make check`
- 联调：`go run ./cmd/server`（加载 `backend/workflows`）+ `npm run dev`，验证流程列表→编辑→校验→测试运行；`python3 scripts/smoke.py` 全绿。

## 风险与处理

- **路由注册冲突**：Gin v1.12 已支持静态/参数共存；以实际构建与 router 测试确认。
- **数字精度回归**：保留原始字节 + `jsonvalue` 路径，并移植相关单测。
- **导入改写遗漏**：机械替换后由 `go build`/`tsc` 兜底，逐项修复。
- **e2e 未移植**：目标工程无 Playwright 配置，本次移植组件/单元测试并用 smoke 脚本覆盖真实链路；Playwright e2e 作为后续可选工作，不影响功能正确性。
- **删除前端 home 演示**：属于清理孤立死代码；如需保留健康检查演示可在审批时提出。
