import { Bindings, JsonBlock, SchemaFields, Section } from './components';
import { descriptorFor } from './graph';
import type { WorkflowNode, WorkflowView } from '@/api/workflow.types';

export default function WorkflowDetails({
  view,
  node,
  onSelect,
  onClear,
}: {
  view: WorkflowView;
  node?: WorkflowNode;
  onSelect: (id: string) => void;
  onClear: () => void;
}) {
  if (!node)
    return (
      <aside className="detail-panel" aria-label="流程详情">
        <div className="detail-heading">
          <span className="eyebrow">WORKFLOW CONTRACT</span>
          <h2>流程契约</h2>
          <p>选择画布中的节点，查看配置、输入来源和执行策略。</p>
        </div>
        <Section title="版本信息">
          <dl className="metadata">
            <dt>流程 ID</dt>
            <dd>
              <code>{view.workflowId}</code>
            </dd>
            <dt>定义版本</dt>
            <dd>
              <code>{view.definitionVersion}</code>
            </dd>
            <dt>协议</dt>
            <dd>
              <code>{view.apiVersion}</code>
            </dd>
          </dl>
        </Section>
        <Section title="流程输入">
          <SchemaFields schema={view.inputSchema} />
        </Section>
        <Section title="流程输出">
          <SchemaFields schema={view.outputSchema} />
          <Bindings bindings={view.outputs} onSelect={onSelect} />
        </Section>
      </aside>
    );
  const descriptor = descriptorFor(view, node);
  const incoming = view.edges.filter((edge) => edge.target === node.id);
  return (
    <aside className="detail-panel" aria-label="节点详情" data-testid="node-detail">
      <div className="detail-heading">
        <div className="heading-row">
          <span className="eyebrow">NODE DETAILS</span>
          <button className="icon-button" onClick={onClear} aria-label="关闭节点详情">
            ×
          </button>
        </div>
        <h2>{node.title || descriptor.title}</h2>
        <p>{descriptor.description}</p>
        <div className="detail-tags">
          <span className="tag">{descriptor.category}</span>
          <span className="tag">v{node.typeVersion}</span>
        </div>
      </div>
      <Section title="节点身份">
        <dl className="metadata">
          <dt>实例 ID</dt>
          <dd>
            <code>{node.id}</code>
          </dd>
          <dt>节点类型</dt>
          <dd>
            <code>{node.type}</code>
          </dd>
          <dt>类型版本</dt>
          <dd>
            <code>{node.typeVersion}</code>
          </dd>
        </dl>
      </Section>
      <Section title="参数配置" count={Object.keys(node.config ?? {}).length}>
        {Object.keys(node.config ?? {}).length ? (
          <JsonBlock value={node.config} />
        ) : (
          <p className="muted">此节点没有配置参数。</p>
        )}
        <SchemaFields
          schema={descriptor.configSchema}
          order={descriptor.uiHints?.fieldOrder}
          advanced={descriptor.uiHints?.advanced}
        />
      </Section>
      <Section title="输入来源" count={Object.keys(node.inputs ?? {}).length}>
        <Bindings bindings={node.inputs} onSelect={onSelect} />
        <SchemaFields schema={descriptor.inputSchema} />
      </Section>
      <Section title="输出字段">
        <SchemaFields schema={descriptor.outputSchema} />
      </Section>
      <Section title="有效执行策略">
        <dl className="metadata">
          <dt>单次超时</dt>
          <dd>{node.timeoutMs.toLocaleString()} ms</dd>
          <dt>最多尝试</dt>
          <dd>{node.retry.maxAttempts} 次</dd>
          <dt>重试间隔</dt>
          <dd>{node.retry.backoffMs.toLocaleString()} ms</dd>
          <dt>重试安全</dt>
          <dd>{descriptor.retrySafe ? '是' : '否'}</dd>
        </dl>
        <p className="subtle-note">仅标记为可重试的错误按策略重试；超时不自动重试。</p>
      </Section>
      <Section title="上游依赖" count={incoming.length}>
        {incoming.length === 0 ? (
          <p className="muted">起始节点，无上游依赖。</p>
        ) : (
          incoming.map((edge) => (
            <div className="dependency" key={edge.id}>
              <button className="text-button mono" onClick={() => onSelect(edge.source)}>
                {edge.source}
              </button>
              <p>
                {edge.reasons
                  ?.map((reason) =>
                    reason === 'data' ? '数据依赖' : reason === 'explicit' ? '顺序依赖' : reason,
                  )
                  .join(' · ') || '依赖'}
              </p>
              {edge.mappings?.map((mapping) => (
                <code key={`${mapping.sourceField}:${mapping.targetField}`}>
                  {mapping.sourceField} → {mapping.targetField}
                </code>
              ))}
            </div>
          ))
        )}
      </Section>
    </aside>
  );
}
