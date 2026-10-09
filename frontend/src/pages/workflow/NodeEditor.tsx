import { useState } from 'react';
import { SchemaFields, Section } from './components';
import { availableOutputs, initialValue, nodeReferences, withBinding } from './draft';
import {
  typeKey,
  type Binding,
  type Definition,
  type JsonValue,
  type NodeDescriptor,
  type Schema,
  type WorkflowNode,
} from '@/api/workflow.types';

type InvalidReporter = (key: string, invalid: boolean) => void;

function JSONEditor({
  label,
  value,
  onChange,
  reportInvalid,
}: {
  label: string;
  value: JsonValue;
  onChange: (value: JsonValue) => void;
  reportInvalid: InvalidReporter;
}) {
  const [text, setText] = useState(() => JSON.stringify(value, null, 2));
  const [error, setError] = useState(false);
  return (
    <label className="editor-field">
      <span>{label}（JSON）</span>
      <textarea
        rows={5}
        className="json-input"
        aria-invalid={error}
        value={text}
        onChange={(event) => {
          setText(event.target.value);
          try {
            const parsed = JSON.parse(event.target.value) as JsonValue;
            setError(false);
            reportInvalid(label, false);
            onChange(parsed);
          } catch {
            setError(true);
            reportInvalid(label, true);
          }
        }}
      />
      {error && (
        <span className="field-error" role="alert">
          请输入有效 JSON；修正后才能校验或运行。
        </span>
      )}
    </label>
  );
}

export function ValueEditor({
  label,
  schema,
  value,
  onChange,
  reportInvalid,
}: {
  label: string;
  schema: Schema;
  value: JsonValue;
  onChange: (value: JsonValue) => void;
  reportInvalid: InvalidReporter;
}) {
  if (schema.enum?.length)
    return (
      <label className="editor-field">
        <span>{label}</span>
        <select
          value={JSON.stringify(value)}
          onChange={(event) => onChange(JSON.parse(event.target.value) as JsonValue)}
        >
          {schema.enum.map((item) => (
            <option key={JSON.stringify(item)} value={JSON.stringify(item)}>
              {JSON.stringify(item)}
            </option>
          ))}
        </select>
      </label>
    );
  if (schema.type === 'boolean')
    return (
      <label className="editor-checkbox">
        <input
          type="checkbox"
          checked={value === true}
          onChange={(event) => onChange(event.target.checked)}
        />
        <span>{label}</span>
      </label>
    );
  if (schema.type === 'string')
    return (
      <label className="editor-field">
        <span>{label}</span>
        <input
          value={typeof value === 'string' ? value : ''}
          onChange={(event) => onChange(event.target.value)}
        />
      </label>
    );
  if (schema.type === 'integer' || schema.type === 'number')
    return (
      <label className="editor-field">
        <span>{label}</span>
        <input
          type="number"
          step={schema.type === 'integer' ? 1 : 'any'}
          min={typeof schema.minimum === 'number' ? schema.minimum : undefined}
          max={typeof schema.maximum === 'number' ? schema.maximum : undefined}
          value={typeof value === 'number' ? value : ''}
          onChange={(event) => onChange(event.target.value === '' ? 0 : Number(event.target.value))}
        />
      </label>
    );
  return <JSONEditor label={label} value={value} onChange={onChange} reportInvalid={reportInvalid} />;
}

export function BindingEditor({
  field,
  schema,
  binding,
  definition,
  catalog,
  currentNodeID,
  onChange,
  reportInvalid,
}: {
  field: string;
  schema: Schema;
  binding?: Binding;
  definition: Definition;
  catalog: NodeDescriptor[];
  currentNodeID?: string;
  onChange: (binding?: Binding) => void;
  reportInvalid: InvalidReporter;
}) {
  const inputs = Object.entries(definition.inputSchema.properties ?? {});
  const outputs = availableOutputs(definition, catalog, currentNodeID);
  return (
    <div className="binding-editor">
      <div className="field-heading">
        <code>{field}</code>
        <span className="type-badge">{schema.type}</span>
      </div>
      <label className="editor-field">
        <span>{field} 来源</span>
        <select
          value={binding?.kind ?? ''}
          onChange={(event) => {
            reportInvalid(`${field} 常量`, false);
            switch (event.target.value) {
              case 'literal':
                onChange({ kind: 'literal', value: initialValue(schema) });
                break;
              case 'workflowInput':
                onChange({ kind: 'workflowInput', field: inputs[0]?.[0] ?? '' });
                break;
              case 'nodeOutput':
                onChange({
                  kind: 'nodeOutput',
                  nodeId: outputs[0]?.nodeId ?? '',
                  field: outputs[0]?.field ?? '',
                });
                break;
              default:
                onChange(undefined);
            }
          }}
        >
          <option value="">未绑定</option>
          <option value="literal">常量</option>
          <option value="workflowInput" disabled={!inputs.length}>
            流程输入
          </option>
          <option value="nodeOutput" disabled={!outputs.length}>
            节点输出
          </option>
        </select>
      </label>
      {binding?.kind === 'workflowInput' && (
        <label className="editor-field">
          <span>{field} 输入字段</span>
          <select
            value={binding.field}
            onChange={(event) => onChange({ kind: 'workflowInput', field: event.target.value })}
          >
            <option value="" disabled>
              选择流程输入字段
            </option>
            {inputs.map(([name, item]) => (
              <option key={name} value={name}>
                {name} · {item.type}
              </option>
            ))}
          </select>
        </label>
      )}
      {binding?.kind === 'nodeOutput' && (
        <label className="editor-field">
          <span>{field} 上游输出</span>
          <select
            value={JSON.stringify([binding.nodeId, binding.field])}
            onChange={(event) => {
              const [nodeId, sourceField] = JSON.parse(event.target.value) as [string, string];
              onChange({ kind: 'nodeOutput', nodeId, field: sourceField });
            }}
          >
            <option value={JSON.stringify(['', ''])} disabled>
              选择来源节点与字段
            </option>
            {outputs.map((item) => (
              <option
                key={JSON.stringify([item.nodeId, item.field])}
                value={JSON.stringify([item.nodeId, item.field])}
              >
                {item.nodeId}.{item.field} · {item.schema.type}
              </option>
            ))}
          </select>
        </label>
      )}
      {binding?.kind === 'literal' && (
        <ValueEditor
          key={`${field}:literal`}
          label={`${field} 常量`}
          schema={schema}
          value={binding.value}
          onChange={(value) => onChange({ kind: 'literal', value })}
          reportInvalid={reportInvalid}
        />
      )}
    </div>
  );
}

export default function NodeEditor({
  definition,
  node,
  catalog,
  onChange,
  onDelete,
  reportInvalid,
  onClose,
}: {
  definition: Definition;
  node: WorkflowNode;
  catalog: NodeDescriptor[];
  onChange: (node: WorkflowNode) => void;
  onDelete: () => void;
  reportInvalid: InvalidReporter;
  onClose: () => void;
}) {
  const descriptor = catalog.find(
    (item) => typeKey(item.type, item.typeVersion) === typeKey(node.type, node.typeVersion),
  );
  const references = nodeReferences(definition, node.id);
  if (!descriptor)
    return (
      <aside className="detail-panel node-editor" aria-label="编辑节点">
        <Section title="节点类型不可用">
          <p className="field-error">
            {node.type}@{node.typeVersion} 未注册，请恢复服务加载版本或重新注册该类型。
          </p>
          <button className="button" onClick={onClose}>
            返回流程设置
          </button>
          <button className="button danger" disabled={references.length > 0} onClick={onDelete}>
            删除节点
          </button>
        </Section>
      </aside>
    );
  return (
    <aside className="detail-panel node-editor" aria-label="编辑节点">
      <div className="detail-heading">
        <div className="heading-row">
          <span className="eyebrow">EDIT NODE</span>
          <button className="icon-button" aria-label="返回流程设置" onClick={onClose}>
            ×
          </button>
        </div>
        <h2>{node.title || descriptor.title}</h2>
        <p>
          <code>{node.id}</code> · {node.type}@{node.typeVersion}
        </p>
      </div>
      <Section title="节点信息">
        <label className="editor-field">
          <span>节点标题</span>
          <input
            value={node.title ?? ''}
            onChange={(event) => onChange({ ...node, title: event.target.value })}
          />
        </label>
        <p className="subtle-note">实例 ID 保持不变；新节点可在添加时设置 ID。</p>
      </Section>
      <Section title="参数配置">
        {Object.entries(descriptor.configSchema.properties ?? {}).length === 0 && (
          <p className="muted">此节点没有配置参数。</p>
        )}
        {Object.entries(descriptor.configSchema.properties ?? {}).map(([field, schema]) => {
          const required = descriptor.configSchema.required?.includes(field);
          const present = Object.hasOwn(node.config ?? {}, field);
          return (
            <div className="config-field" key={field}>
              {!required && (
                <label className="editor-checkbox">
                  <input
                    type="checkbox"
                    checked={present}
                    onChange={(event) => {
                      const config = { ...node.config };
                      if (event.target.checked) config[field] = initialValue(schema);
                      else {
                        delete config[field];
                        reportInvalid(`配置 ${field}`, false);
                      }
                      onChange({ ...node, config });
                    }}
                  />
                  <span>设置 {field}</span>
                </label>
              )}
              {(required || present) && (
                <ValueEditor
                  label={`配置 ${field}`}
                  schema={schema}
                  value={node.config?.[field] ?? initialValue(schema)}
                  onChange={(value) => onChange({ ...node, config: { ...node.config, [field]: value } })}
                  reportInvalid={reportInvalid}
                />
              )}
              {schema.description && <p className="subtle-note">{schema.description}</p>}
            </div>
          );
        })}
      </Section>
      <Section title="输入绑定">
        {Object.entries(descriptor.inputSchema.properties ?? {}).map(([field, schema]) => (
          <BindingEditor
            key={field}
            field={field}
            schema={schema}
            binding={node.inputs?.[field]}
            definition={definition}
            catalog={catalog}
            currentNodeID={node.id}
            onChange={(binding) => onChange({ ...node, inputs: withBinding(node.inputs, field, binding) })}
            reportInvalid={reportInvalid}
          />
        ))}
        <p className="subtle-note">数据依赖随节点输出绑定自动生成。类型兼容性和循环依赖由校验检查。</p>
      </Section>
      <Section title="顺序依赖">
        {definition.nodes
          .filter((item) => item.id !== node.id)
          .map((item) => (
            <label className="editor-checkbox" key={item.id}>
              <input
                type="checkbox"
                checked={node.dependsOn?.includes(item.id) ?? false}
                onChange={(event) =>
                  onChange({
                    ...node,
                    dependsOn: event.target.checked
                      ? [...(node.dependsOn ?? []), item.id]
                      : (node.dependsOn ?? []).filter((id) => id !== item.id),
                  })
                }
              />
              <span>{item.id}</span>
            </label>
          ))}
        <p className="subtle-note">仅补充执行顺序；无需重复勾选输入绑定已建立的依赖。</p>
      </Section>
      <Section title="执行策略">
        <label className="editor-field">
          <span>单次超时（ms）</span>
          <input
            type="number"
            min={1}
            max={120000}
            step={1}
            value={node.timeoutMs}
            onChange={(event) => onChange({ ...node, timeoutMs: Number(event.target.value) })}
          />
        </label>
        <label className="editor-field">
          <span>最多尝试次数</span>
          <input
            type="number"
            min={1}
            max={descriptor.retrySafe ? 3 : 1}
            step={1}
            value={node.retry.maxAttempts}
            onChange={(event) =>
              onChange({ ...node, retry: { ...node.retry, maxAttempts: Number(event.target.value) } })
            }
          />
        </label>
        <label className="editor-field">
          <span>重试间隔（ms）</span>
          <input
            type="number"
            min={1}
            max={10000}
            step={1}
            value={node.retry.backoffMs}
            onChange={(event) =>
              onChange({ ...node, retry: { ...node.retry, backoffMs: Number(event.target.value) } })
            }
          />
        </label>
        <p className="subtle-note">
          {descriptor.retrySafe
            ? '可重试错误最多尝试 3 次；超时不重试。'
            : '该节点未声明重试安全，最多尝试 1 次。'}
        </p>
      </Section>
      <Section title="输出契约">
        <SchemaFields schema={descriptor.outputSchema} />
      </Section>
      <Section title="删除节点">
        <button
          className="button danger"
          disabled={references.length > 0 || definition.nodes.length <= 1}
          onClick={onDelete}
        >
          删除节点
        </button>
        {references.length > 0 && (
          <p className="subtle-note">先修改以下引用后才能删除：{references.join('、')}。</p>
        )}
        {definition.nodes.length <= 1 && <p className="subtle-note">流程至少保留一个节点。</p>}
      </Section>
    </aside>
  );
}
