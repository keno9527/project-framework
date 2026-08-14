# Frontend Agent Guide

本文件适用于 `frontend/`。根目录 `AGENTS.md` 的共同规则继续生效；发生冲突时，以本文件为准。

## 启动路径

1. 阅读 `frontend/README.md`、`package.json` 和目标模块的现有测试。
2. 从 `src/main.tsx`、`src/App.tsx` 沿页面到 `src/api` 追踪用户状态和请求链路。
3. 确认加载、成功、失败、卸载和过期请求等边界状态。
4. 先运行最小相关测试确认基线；行为变更先写最小失败测试。

## 模块边界

| 路径 | 职责 | 约束 |
| --- | --- | --- |
| `src/main.tsx` | 浏览器入口和 React 挂载 | 不承载页面业务 |
| `src/App.tsx` | 应用装配 | 不堆放具体页面逻辑 |
| `src/pages/<feature>` | 页面视图、状态、局部 hook 和样式 | 按功能内聚 |
| `src/api` | 后端请求、响应校验和统一请求错误 | 不依赖页面 |
| `src/styles.css` | 全局基础样式和设计变量 | 页面样式优先就近存放 |
| `src/test` | Vitest 共享初始化 | 不放业务测试辅助逻辑 |

依赖方向保持 `main.tsx -> App.tsx -> pages/<feature> -> api -> backend`。局部能力只有出现真实跨页面复用时才提升为全局模块。

## 实施规则

### TypeScript 与状态

- 保持严格类型检查，不使用 `any`、非必要类型断言或 `@ts-ignore` 绕过边界。
- 后端响应先按 `unknown` 接收，在 `src/api` 完成运行时校验和错误转换。
- 页面覆盖加载、成功和失败状态；异步副作用在卸载时取消或显式忽略过期结果。
- 页面不重复拼接 API URL、请求头或底层响应错误。

### 页面与样式

- 页面按 `src/pages/<feature>` 内聚；`hooks/`、局部样式和测试按需创建，不创建空目录。
- 不预先增加全局 `components`、`layouts`、`router`、`utils` 或状态目录。
- 复用 `src/styles.css` 中的设计变量，保证最窄 `20rem` 视口可用，并尊重 `prefers-reduced-motion`。
- 使用语义化 HTML，以可访问文本或适当的 ARIA 属性表达动态状态。
- 不手工修改 `dist`、`node_modules` 或 TypeScript 构建缓存。

## 测试与验证

组件测试关注用户可见行为，优先使用语义角色和可见文本；API 测试覆盖请求、成功校验、HTTP 错误和非法响应。可以 mock `src/api` 边界，但不要 mock 被测页面内部私有函数。

先执行目标测试，完成前在仓库根目录运行 `make check-frontend`；API 契约变化按根目录规则执行 `make check` 和本地联调。
