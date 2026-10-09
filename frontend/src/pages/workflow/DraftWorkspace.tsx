import { useCallback, useEffect, useRef, useState } from 'react';
import { errorMessage, postData, useAPI } from '@/api/workflow';
import { SchemaFields, Section } from './components';
import {
  createNode,
  definitionFromView,
  draftStorageKey,
  hasUnsafeNumber,
  nodeReferences,
  readDraft,
  saveDraft,
  UNSAFE_NUMBER_MESSAGE,
  withBinding,
} from './draft';
import { validateWorkflow } from './graph';
import NodeEditor, { BindingEditor } from './NodeEditor';
import RunPanel from './RunPanel';
import { typeKey, type Definition, type NodeDescriptor, type WorkflowView } from '@/api/workflow.types';
import WorkflowCanvas from './WorkflowCanvas';
import WorkflowDetails from './WorkflowDetails';

function validateCatalog(data: { items: NodeDescriptor[] }) {
  if (
    !data ||
    !Array.isArray(data.items) ||
    data.items.some(
      (item) =>
        !item?.type || !item.typeVersion || !item.configSchema || !item.inputSchema || !item.outputSchema,
    )
  ) {
    throw new Error('节点目录数据不完整，无法添加节点。');
  }
}

export default function DraftWorkspace({ view }: { view: WorkflowView }) {
  const [initial] = useState(() => readDraft(view));
  const [definition, setDefinition] = useState(initial.definition);
  const [editing, setEditing] = useState(initial.restored);
  const [edited, setEdited] = useState(initial.restored);
  const [revision, setRevision] = useState(0);
  const revisionRef = useRef(0);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [preview, setPreview] = useState(view);
  const [validatedRevision, setValidatedRevision] = useState(initial.restored ? -1 : 0);
  const [validationError, setValidationError] = useState('');
  const [validating, setValidating] = useState(false);
  const [notice, setNotice] = useState(
    initial.notice ?? (initial.restored ? '已恢复此浏览器保存的草稿，请校验后查看最新流程图。' : ''),
  );
  const [saveFailed, setSaveFailed] = useState(false);
  const [formErrors, setFormErrors] = useState<string[]>([]);
  const [generation, setGeneration] = useState(0);
  const [showRun, setShowRun] = useState(false);
  const [showAdd, setShowAdd] = useState(false);
  const [newType, setNewType] = useState('');
  const [newID, setNewID] = useState('');
  const [addError, setAddError] = useState('');
  const [confirmReset, setConfirmReset] = useState(false);
  const pending = useRef<AbortController | null>(null);
  const runContainer = useRef<HTMLDivElement | null>(null);
  const { state: catalogState, retry: retryCatalog } = useAPI<{ items: NodeDescriptor[] }>(
    '/api/v1/node-types',
    validateCatalog,
  );
  const catalog = catalogState.status === 'ready' ? catalogState.data.items : view.nodeTypes;
  const unsafeNumbers = hasUnsafeNumber(definition);

  useEffect(() => () => pending.current?.abort(), []);
  useEffect(() => {
    if (showRun) runContainer.current?.scrollIntoView?.({ block: 'start' });
  }, [showRun]);

  function update(next: Definition) {
    pending.current?.abort();
    setValidating(false);
    revisionRef.current++;
    setRevision(revisionRef.current);
    setDefinition(next);
    setEdited(true);
    setValidationError('');
    const error = saveDraft(next);
    setSaveFailed(Boolean(error));
    setNotice(error ?? '草稿已自动保存到此浏览器。');
  }

  function reportInvalid(key: string, invalid: boolean) {
    if (invalid) {
      pending.current?.abort();
      setValidating(false);
    }
    setFormErrors((errors) =>
      invalid ? [...new Set([...errors, key])] : errors.filter((item) => item !== key),
    );
  }

  const select = useCallback(
    (id: string) => {
      if (formErrors.length) {
        setNotice('请先修正当前字段的 JSON，或恢复服务加载版本。');
        return;
      }
      setSelectedID(id);
    },
    [formErrors.length],
  );
  const clear = useCallback(() => {
    if (formErrors.length) {
      setNotice('请先修正当前字段的 JSON，或恢复服务加载版本。');
      return;
    }
    setSelectedID(null);
  }, [formErrors.length]);

  async function validate(exportAfter = false) {
    if (unsafeNumbers || formErrors.length) return;
    pending.current?.abort();
    const controller = new AbortController();
    pending.current = controller;
    const requestedRevision = revisionRef.current;
    setValidating(true);
    setValidationError('');
    try {
      const result = await postData<{ view: WorkflowView; yaml: string }>(
        '/api/v1/workflows/validate',
        { definition },
        controller.signal,
      );
      if (controller.signal.aborted || requestedRevision !== revisionRef.current) return;
      validateWorkflow(result.view);
      if (typeof result.yaml !== 'string') throw new Error('服务返回的 YAML 数据无效。');
      setPreview(result.view);
      setValidatedRevision(requestedRevision);
      setNotice(
        exportAfter
          ? '已导出校验通过的 YAML；放入配置目录并重启服务后加载。'
          : '草稿校验通过，流程图已更新。',
      );
      if (exportAfter) {
        const url = URL.createObjectURL(new Blob([result.yaml], { type: 'application/yaml;charset=utf-8' }));
        const anchor = document.createElement('a');
        anchor.href = url;
        anchor.download = `${definition.workflowId.replace(/[^a-zA-Z0-9_.-]/g, '_')}-draft.yaml`;
        anchor.click();
        URL.revokeObjectURL(url);
      }
    } catch (error) {
      if (!controller.signal.aborted && requestedRevision === revisionRef.current)
        setValidationError(errorMessage(error));
    } finally {
      if (!controller.signal.aborted && requestedRevision === revisionRef.current) setValidating(false);
    }
  }

  function reset() {
    pending.current?.abort();
    revisionRef.current++;
    setRevision(revisionRef.current);
    setValidatedRevision(revisionRef.current);
    setDefinition(definitionFromView(view));
    setPreview(view);
    setEdited(false);
    setSelectedID(null);
    setFormErrors([]);
    setValidationError('');
    setValidating(false);
    setConfirmReset(false);
    setGeneration((value) => value + 1);
    try {
      localStorage.removeItem(draftStorageKey(view));
      setNotice('已恢复服务加载版本，并清除此浏览器草稿。');
      setSaveFailed(false);
    } catch {
      setNotice('已恢复服务加载版本，但浏览器中的旧草稿无法清除。');
      setSaveFailed(true);
    }
  }

  function addNode() {
    const id = newID.trim();
    const descriptor = catalog.find((item) => typeKey(item.type, item.typeVersion) === newType) ?? catalog[0];
    if (!descriptor) {
      setAddError('请选择可用节点类型。');
      return;
    }
    if (!/^[A-Za-z][A-Za-z0-9-]*$/.test(id)) {
      setAddError('节点 ID 需以字母开头，只能包含字母、数字和连字符。');
      return;
    }
    if (definition.nodes.some((node) => node.id === id)) {
      setAddError('节点 ID 已存在，请使用唯一 ID。');
      return;
    }
    if (definition.nodes.length >= 50) {
      setAddError('流程最多支持 50 个节点。');
      return;
    }
    update({ ...definition, nodes: [...definition.nodes, createNode(descriptor, id)] });
    setSelectedID(id);
    setShowAdd(false);
    setNewID('');
    setAddError('');
  }

  const selectedNode = definition.nodes.find((node) => node.id === selectedID);
  const previewCurrent = validatedRevision === revision && !formErrors.length;

  return (
    <div className="draft-workspace">
      <div className="draft-toolbar">
        <div className="mode-tabs" aria-label="流程工作模式">
          <button
            className={!editing ? 'active' : ''}
            aria-pressed={!editing}
            disabled={formErrors.length > 0}
            onClick={() => setEditing(false)}
          >
            查看流程
          </button>
          <button className={editing ? 'active' : ''} aria-pressed={editing} onClick={() => setEditing(true)}>
            编辑节点
          </button>
        </div>
        <div className="draft-actions">
          {editing && (
            <>
              <button
                className="button"
                disabled={validating || formErrors.length > 0 || unsafeNumbers}
                onClick={() => void validate()}
              >
                {validating ? '校验中…' : '校验草稿'}
              </button>
              <button
                className="button"
                disabled={validating || formErrors.length > 0 || unsafeNumbers}
                onClick={() => void validate(true)}
              >
                导出 YAML
              </button>
            </>
          )}
          <button
            className={`button ${showRun ? 'primary' : ''}`}
            aria-expanded={showRun}
            onClick={() => setShowRun((value) => !value)}
          >
            ▶ 测试运行
          </button>
        </div>
      </div>
      {(editing || edited) && (
        <div className={`draft-status ${saveFailed ? 'warning' : ''}`}>
          <div>
            <strong>{edited ? '浏览器草稿' : '基于已加载版本'}</strong>
            <span>{notice || '修改自动保存到此浏览器；导出 YAML 后可部署。服务已加载版本保持不变。'}</span>
          </div>
          <button className="text-button" onClick={() => setConfirmReset(true)}>
            恢复加载版本
          </button>
        </div>
      )}
      {confirmReset && (
        <div className="inline-confirm" role="alert">
          <span>将丢弃此流程的浏览器草稿和未完成编辑，恢复服务当前加载版本。</span>
          <button className="button danger" onClick={reset}>
            丢弃草稿并恢复
          </button>
          <button className="button" onClick={() => setConfirmReset(false)}>
            继续编辑
          </button>
        </div>
      )}
      {validationError && (
        <div className="draft-error" role="alert">
          <strong>草稿未通过校验</strong>
          <span>{validationError}</span>
          <small>修改仍保留；下方画布展示最近一次通过校验的结构。</small>
        </div>
      )}
      {unsafeNumbers && (
        <div className="draft-error" role="alert">
          {UNSAFE_NUMBER_MESSAGE}
        </div>
      )}
      <div className="workflow-content">
        <div className="graph-column draft-graph-column">
          <div className="graph-toolbar">
            <div>
              <span className="section-label">流程结构</span>
              <span className="graph-count">
                {preview.nodes.length} 个节点 <span>·</span> {preview.edges.length} 条依赖
              </span>
            </div>
            <span className={`readonly-badge ${!previewCurrent ? 'stale-badge' : ''}`}>
              {previewCurrent ? (edited ? '草稿已校验' : '已加载版本') : '上次有效预览 · 草稿待校验'}
            </span>
          </div>
          {editing && (
            <div className="draft-node-list" aria-label="草稿节点列表">
              {definition.nodes.map((node) => (
                <button
                  key={node.id}
                  className={selectedID === node.id ? 'active' : ''}
                  aria-pressed={selectedID === node.id}
                  aria-label={`编辑节点 ${node.id}`}
                  onClick={() => select(node.id)}
                >
                  {node.id}
                </button>
              ))}
              <button
                className="add-node-button"
                disabled={formErrors.length > 0 || definition.nodes.length >= 50}
                onClick={() => setShowAdd((value) => !value)}
              >
                ＋ 添加节点
              </button>
              <button onClick={clear}>流程输出设置</button>
            </div>
          )}
          {showAdd && editing && (
            <div className="add-node-form">
              <label className="editor-field">
                <span>节点类型</span>
                <select
                  value={newType || (catalog[0] ? typeKey(catalog[0].type, catalog[0].typeVersion) : '')}
                  onChange={(event) => setNewType(event.target.value)}
                >
                  {catalog.map((item) => (
                    <option
                      key={typeKey(item.type, item.typeVersion)}
                      value={typeKey(item.type, item.typeVersion)}
                    >
                      {item.title} · {item.type}@{item.typeVersion}
                    </option>
                  ))}
                </select>
              </label>
              <label className="editor-field">
                <span>新节点 ID</span>
                <input
                  value={newID}
                  placeholder="例如 format-result"
                  onChange={(event) => setNewID(event.target.value)}
                />
              </label>
              <button className="button primary" onClick={addNode}>
                确认添加
              </button>
              <button className="button" onClick={() => setShowAdd(false)}>
                取消
              </button>
              {catalogState.status === 'error' && (
                <p className="field-error">
                  完整目录加载失败，目前仅显示此流程已使用类型。
                  <button className="text-button" onClick={retryCatalog}>
                    重试目录
                  </button>
                </p>
              )}
              {addError && (
                <p className="field-error" role="alert">
                  {addError}
                </p>
              )}
            </div>
          )}
          <WorkflowCanvas view={preview} selectedId={selectedID} onSelect={select} onClear={clear} />
          <div className="graph-footer">
            <span className="legend-line" />
            {editing
              ? '在右侧表单修改绑定和依赖；校验通过后更新流程图。'
              : '箭头表示执行依赖；所有上游成功后执行下游。'}
          </div>
          <div hidden={!showRun} className="run-panel-container" ref={runContainer}>
            <RunPanel
              definition={definition}
              revision={revision}
              disabled={formErrors.length > 0 || unsafeNumbers}
            />
          </div>
        </div>
        {editing ? (
          selectedNode ? (
            <NodeEditor
              key={`${generation}:${selectedNode.id}`}
              definition={definition}
              node={selectedNode}
              catalog={catalog}
              onChange={(node) =>
                update({
                  ...definition,
                  nodes: definition.nodes.map((item) => (item.id === node.id ? node : item)),
                })
              }
              reportInvalid={reportInvalid}
              onClose={clear}
              onDelete={() => {
                if (nodeReferences(definition, selectedNode.id).length || definition.nodes.length <= 1)
                  return;
                update({
                  ...definition,
                  nodes: definition.nodes.filter((node) => node.id !== selectedNode.id),
                });
                setSelectedID(null);
              }}
            />
          ) : (
            <aside
              className="detail-panel node-editor"
              aria-label="编辑流程设置"
              key={`workflow:${generation}`}
            >
              <div className="detail-heading">
                <span className="eyebrow">EDIT WORKFLOW</span>
                <h2>流程设置</h2>
                <p>选择节点编辑参数，或调整流程最终输出绑定。</p>
              </div>
              <Section title="流程信息">
                <label className="editor-field">
                  <span>流程标题</span>
                  <input
                    value={definition.title}
                    onChange={(event) => update({ ...definition, title: event.target.value })}
                  />
                </label>
                <p className="subtle-note">
                  {definition.workflowId} · v{definition.definitionVersion}
                </p>
              </Section>
              <Section title="流程输入">
                <SchemaFields schema={definition.inputSchema} />
              </Section>
              <Section title="流程输出绑定">
                {Object.entries(definition.outputSchema.properties ?? {}).map(([field, schema]) => (
                  <BindingEditor
                    key={field}
                    field={field}
                    schema={schema}
                    binding={definition.outputs?.[field]}
                    definition={definition}
                    catalog={catalog}
                    onChange={(binding) =>
                      update({ ...definition, outputs: withBinding(definition.outputs, field, binding) })
                    }
                    reportInvalid={reportInvalid}
                  />
                ))}
              </Section>
              <Section title="草稿使用说明">
                <p className="muted">
                  选择「测试运行」执行当前草稿。新增节点需完成输入绑定；流程输出可改为引用新节点。校验、测试和导出不会修改服务已加载的定义。
                </p>
              </Section>
            </aside>
          )
        ) : (
          <WorkflowDetails
            view={preview}
            node={preview.nodes.find((node) => node.id === selectedID)}
            onSelect={select}
            onClear={clear}
          />
        )}
      </div>
    </div>
  );
}
