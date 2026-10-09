import { useState } from 'react';
import { useAPI } from '@/api/workflow';
import { SchemaFields, Section, StatePanel } from './components';
import { validateWorkflow } from './graph';
import { typeKey, workflowKey, type NodeDescriptor, type WorkflowSummary, type WorkflowView } from '@/api/workflow.types';
import DraftWorkspace from './DraftWorkspace';
import './workflow.css';

function validateSummaries(data: { items: WorkflowSummary[] }) {
  if (
    !data ||
    !Array.isArray(data.items) ||
    data.items.some(
      (item) =>
        !item ||
        typeof item.workflowId !== 'string' ||
        typeof item.definitionVersion !== 'string' ||
        typeof item.title !== 'string' ||
        !Number.isInteger(item.nodeCount),
    )
  ) {
    throw new Error('流程列表数据异常，请检查服务响应。');
  }
}

function validateCatalog(data: { items: NodeDescriptor[] }) {
  if (
    !data ||
    !Array.isArray(data.items) ||
    data.items.some(
      (item) =>
        !item ||
        typeof item.type !== 'string' ||
        typeof item.title !== 'string' ||
        typeof item.typeVersion !== 'string' ||
        typeof item.description !== 'string' ||
        typeof item.category !== 'string' ||
        !item.inputSchema ||
        !item.outputSchema ||
        !item.configSchema,
    )
  ) {
    throw new Error('节点目录数据异常：节点描述缺失或不完整。');
  }
}

function WorkflowWorkspace({ workflow }: { workflow: WorkflowSummary }) {
  const path = `/api/v1/workflows/${encodeURIComponent(workflow.workflowId)}/versions/${encodeURIComponent(workflow.definitionVersion)}`;
  const { state, retry } = useAPI<WorkflowView>(path, validateWorkflow);
  return (
    <>
      <header className="workspace-header">
        <div>
          <div className="breadcrumb">
            工作空间 <span>/</span> 流程定义
          </div>
          <div className="title-row">
            <h1>{workflow.title}</h1>
            <span className="version-badge">v{workflow.definitionVersion}</span>
          </div>
          <p>
            <code>{workflow.workflowId}</code>
            <span>编辑节点、校验流程草稿并执行测试</span>
          </p>
        </div>
        <button className="button subtle" onClick={retry} aria-label="重新加载流程详情">
          ↻ <span>刷新定义</span>
        </button>
      </header>
      {state.status === 'loading' && (
        <StatePanel title="正在读取流程定义…">加载节点描述及依赖关系。</StatePanel>
      )}
      {state.status === 'error' && (
        <StatePanel title="无法展示流程" error retry={retry}>
          {state.error}
        </StatePanel>
      )}
      {state.status === 'ready' &&
        (state.data.workflowId === workflow.workflowId &&
        state.data.definitionVersion === workflow.definitionVersion ? (
          <DraftWorkspace key={`${path}`} view={state.data} />
        ) : (
          <StatePanel title="流程数据异常" error retry={retry}>
            服务返回的流程版本与所选版本不一致。
          </StatePanel>
        ))}
    </>
  );
}

function Catalog() {
  const { state, retry } = useAPI<{ items: NodeDescriptor[] }>('/api/v1/node-types', validateCatalog);
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState<string | null>(null);
  const items = state.status === 'ready' ? state.data.items : [];
  const filtered = items.filter((item) =>
    `${item.title} ${item.type} ${item.category}`
      .toLocaleLowerCase()
      .includes(query.trim().toLocaleLowerCase()),
  );
  const descriptor =
    filtered.find((item) => typeKey(item.type, item.typeVersion) === selected) ?? filtered[0];
  return (
    <>
      <header className="workspace-header">
        <div>
          <div className="breadcrumb">
            工作空间 <span>/</span> 能力目录
          </div>
          <div className="title-row">
            <h1>节点目录</h1>
            <span className="version-badge">{items.length} 个类型版本</span>
          </div>
          <p>已注册的节点能力与数据契约，可在流程配置中按类型和版本引用。</p>
        </div>
        <button className="button subtle" onClick={retry}>
          ↻ 刷新目录
        </button>
      </header>
      {state.status === 'loading' && <StatePanel title="正在加载节点目录…" />}
      {state.status === 'error' && (
        <StatePanel title="无法读取节点目录" error retry={retry}>
          {state.error}
        </StatePanel>
      )}
      {state.status === 'ready' && (
        <div className="catalog-content">
          <div className="catalog-list">
            <label className="search-label">
              <span>搜索节点</span>
              <input
                type="search"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="搜索名称、类型或分类"
              />
            </label>
            <div className="catalog-count">{filtered.length} 个节点类型版本</div>
            {filtered.length === 0 ? (
              <StatePanel title={items.length ? '没有匹配的节点' : '暂无可用节点'}>
                {items.length ? '尝试其他名称、类型或分类。' : '服务尚未提供已注册的节点描述。'}
              </StatePanel>
            ) : (
              <div className="catalog-grid">
                {filtered.map((item) => (
                  <button
                    key={typeKey(item.type, item.typeVersion)}
                    className={`catalog-card ${item === descriptor ? 'active' : ''}`}
                    aria-pressed={item === descriptor}
                    onClick={() => setSelected(typeKey(item.type, item.typeVersion))}
                  >
                    <span className="catalog-card-top">
                      <span className="node-symbol" aria-hidden="true">
                        ◇
                      </span>
                      <span className="tag">{item.category}</span>
                      <span className="catalog-version">v{item.typeVersion}</span>
                    </span>
                    <strong>{item.title}</strong>
                    <code>{item.type}</code>
                    <p>{item.description}</p>
                    <span className="catalog-card-bottom">
                      查看数据契约 <span aria-hidden="true">↗</span>
                    </span>
                  </button>
                ))}
              </div>
            )}
          </div>
          {descriptor && (
            <aside className="detail-panel" aria-label="节点类型详情">
              <div className="detail-heading">
                <span className="eyebrow">NODE TYPE</span>
                <h2>{descriptor.title}</h2>
                <p>{descriptor.description}</p>
              </div>
              <Section title="类型信息">
                <dl className="metadata">
                  <dt>类型</dt>
                  <dd>
                    <code>{descriptor.type}</code>
                  </dd>
                  <dt>类型版本</dt>
                  <dd>{descriptor.typeVersion}</dd>
                  <dt>分类</dt>
                  <dd>{descriptor.category}</dd>
                  <dt>重试安全</dt>
                  <dd>{descriptor.retrySafe ? '是' : '否'}</dd>
                </dl>
              </Section>
              <Section title="配置契约">
                <SchemaFields
                  schema={descriptor.configSchema}
                  order={descriptor.uiHints?.fieldOrder}
                  advanced={descriptor.uiHints?.advanced}
                />
              </Section>
              <Section title="输入契约">
                <SchemaFields schema={descriptor.inputSchema} />
              </Section>
              <Section title="输出契约">
                <SchemaFields schema={descriptor.outputSchema} />
              </Section>
            </aside>
          )}
        </div>
      )}
    </>
  );
}

export function WorkflowConsole() {
  const { state, retry } = useAPI<{ items: WorkflowSummary[] }>('/api/v1/workflows', validateSummaries);
  const [page, setPage] = useState<'workflows' | 'catalog'>('workflows');
  const [selected, setSelected] = useState<string | null>(null);
  const items = state.status === 'ready' ? state.data.items : [];
  const workflow = items.find((item) => workflowKey(item) === selected) ?? items[0];
  return (
    <div className="app-shell">
      <aside className="sidebar" aria-label="工作空间导航">
        <a className="brand" href="/" aria-label="Workflow 主页">
          <span className="brand-mark" aria-hidden="true">
            W
          </span>
          <span>
            workflow<span className="brand-dot">.</span>
            <small>DEVELOPER CONSOLE</small>
          </span>
        </a>
        <div className="workspace-label">
          <span className="workspace-avatar">W</span>
          <div>
            开发工作空间<small>本地 / 隔离测试环境</small>
          </div>
          <span className="tag">V1</span>
        </div>
        <nav className="primary-nav">
          <button
            className={page === 'workflows' ? 'active' : ''}
            aria-current={page === 'workflows' ? 'page' : undefined}
            onClick={() => setPage('workflows')}
          >
            <span aria-hidden="true">⌘</span>流程定义<span className="nav-count">{items.length}</span>
          </button>
          <button
            className={page === 'catalog' ? 'active' : ''}
            aria-current={page === 'catalog' ? 'page' : undefined}
            onClick={() => setPage('catalog')}
          >
            <span aria-hidden="true">▦</span>节点目录
          </button>
        </nav>
        <div className="sidebar-section-heading">
          <span>已加载流程</span>
          <button className="icon-button" aria-label="重新加载流程列表" onClick={retry}>
            ↻
          </button>
        </div>
        <div className="workflow-list">
          {state.status === 'loading' && (
            <p className="sidebar-message" role="status">
              正在读取…
            </p>
          )}
          {state.status === 'error' && (
            <div className="sidebar-message" role="alert">
              <p>{state.error}</p>
              <button className="text-button" onClick={retry}>
                重试
              </button>
            </div>
          )}
          {state.status === 'ready' && items.length === 0 && (
            <p className="sidebar-message">暂无已加载的流程。</p>
          )}
          {items.map((item) => (
            <button
              className={`workflow-list-item ${page === 'workflows' && workflow === item ? 'active' : ''}`}
              key={workflowKey(item)}
              aria-pressed={page === 'workflows' && workflow === item}
              onClick={() => {
                setSelected(workflowKey(item));
                setPage('workflows');
              }}
            >
              <span className="workflow-item-heading">
                <span className="workflow-indicator" aria-hidden="true" />
                <strong>{item.title}</strong>
                <span className="list-version">v{item.definitionVersion}</span>
              </span>
              <code>{item.workflowId}</code>
              <span className="workflow-item-meta">{item.nodeCount} 个节点</span>
            </button>
          ))}
        </div>
        <div className="sidebar-footer">
          <span className="footer-label">
            <span className="live-dot" />
            本地草稿与测试
          </span>
          <p>
            配置于服务启动时加载。
            <br />
            编辑草稿，验证后导出 YAML。
          </p>
        </div>
      </aside>
      <main className="workspace">
        {page === 'catalog' ? (
          <Catalog />
        ) : workflow ? (
          <WorkflowWorkspace key={workflowKey(workflow)} workflow={workflow} />
        ) : state.status === 'loading' ? (
          <StatePanel title="正在连接工作空间…" />
        ) : state.status === 'error' ? (
          <StatePanel title="无法读取流程列表" error retry={retry}>
            {state.error}
          </StatePanel>
        ) : (
          <StatePanel title="暂无已加载流程" retry={retry}>
            将有效的 YAML 配置放入流程目录并重新启动后端服务，然后刷新列表。
          </StatePanel>
        )}
      </main>
    </div>
  );
}
