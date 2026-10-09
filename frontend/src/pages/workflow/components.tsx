import type { ReactNode } from 'react';
import type { Binding, Schema } from '@/api/workflow.types';

export function StatePanel({
  title,
  children,
  retry,
  error = false,
}: {
  title: string;
  children?: ReactNode;
  retry?: () => void;
  error?: boolean;
}) {
  return (
    <div className={`state-panel ${error ? 'state-error' : ''}`} role={error ? 'alert' : 'status'}>
      <span className="state-mark" aria-hidden="true">
        {error ? '!' : '◇'}
      </span>
      <strong>{title}</strong>
      {children && <p>{children}</p>}
      {retry && (
        <button className="button" onClick={retry}>
          重新加载
        </button>
      )}
    </div>
  );
}

export function JsonBlock({ value }: { value: unknown }) {
  return <pre className="json-block">{JSON.stringify(value, null, 2)}</pre>;
}

export function Section({ title, children, count }: { title: string; children: ReactNode; count?: number }) {
  return (
    <section className="detail-section">
      <h3>
        {title}
        {count !== undefined && <span className="count">{count}</span>}
      </h3>
      {children}
    </section>
  );
}

export function SchemaFields({
  schema,
  order = [],
  advanced = [],
}: {
  schema: Schema;
  order?: string[];
  advanced?: string[];
}) {
  const fields = Object.entries(schema.properties ?? {});
  fields.sort(([a], [b]) => {
    const ai = order.indexOf(a),
      bi = order.indexOf(b);
    return (ai < 0 ? Infinity : ai) - (bi < 0 ? Infinity : bi);
  });
  return (
    <>
      {fields.length === 0 ? (
        <p className="muted">无字段。</p>
      ) : (
        <div className="schema-fields">
          {fields.map(([name, field]) => (
            <div className="schema-field" key={name}>
              <div className="field-heading">
                <code>{name}</code>
                <span className="type-badge">
                  {field.type === 'array' ? `${field.items?.type ?? 'unknown'}[]` : field.type}
                </span>
                {schema.required?.includes(name) && <span className="required">必填</span>}
                {advanced.includes(name) && <span className="required">高级</span>}
              </div>
              {(field.title || field.description) && <p>{field.description || field.title}</p>}
              {field.enum && (
                <div className="constraint">
                  枚举：<code>{field.enum.map((value) => JSON.stringify(value)).join(' / ')}</code>
                </div>
              )}
              {Object.hasOwn(field, 'default') && (
                <div className="constraint">
                  Schema 默认值（仅描述）：<code>{JSON.stringify(field.default)}</code>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
      <details className="schema-source">
        <summary>完整 Schema 与约束</summary>
        <JsonBlock value={schema} />
      </details>
    </>
  );
}

export function BindingValue({ binding, onSelect }: { binding: Binding; onSelect?: (id: string) => void }) {
  if (binding.kind === 'literal')
    return (
      <>
        <span className="binding-kind">常量</span>
        <code className="binding-value">{JSON.stringify(binding.value)}</code>
      </>
    );
  if (binding.kind === 'workflowInput')
    return (
      <>
        <span className="binding-kind">流程输入</span>
        <code className="binding-value">{binding.field}</code>
      </>
    );
  return (
    <>
      <span className="binding-kind">节点输出</span>
      {onSelect ? (
        <button className="text-button mono" onClick={() => onSelect(binding.nodeId)}>
          {binding.nodeId}.{binding.field}
        </button>
      ) : (
        <code>
          {binding.nodeId}.{binding.field}
        </code>
      )}
    </>
  );
}

export function Bindings({
  bindings,
  onSelect,
}: {
  bindings?: Record<string, Binding>;
  onSelect?: (id: string) => void;
}) {
  const entries = Object.entries(bindings ?? {});
  return entries.length ? (
    <div className="bindings">
      {entries.map(([field, binding]) => (
        <div className="binding-row" key={field}>
          <code className="binding-target">{field}</code>
          <span className="binding-arrow" aria-hidden="true">
            ←
          </span>
          <div>
            <BindingValue binding={binding} onSelect={onSelect} />
          </div>
        </div>
      ))}
    </div>
  ) : (
    <p className="muted">无字段绑定。</p>
  );
}
