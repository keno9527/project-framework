import { useEffect, useRef, useState } from 'react';
import { APIError, errorMessage, fetchData, postJSONData } from '@/api/workflow';
import { JsonBlock } from './components';
import { hasUnsafeNumber } from './draft';
import type { Definition, JsonValue, RunSnapshot, RunStatus, Schema } from '@/api/workflow.types';
import './run-panel.css';

const labels: Record<RunStatus, string> = {
  PENDING: '等待执行',
  RUNNING: '执行中',
  RETRY_WAIT: '等待重试',
  SUCCEEDED: '成功',
  FAILED: '失败',
  TIMED_OUT: '超时',
  CANCELLED: '已取消',
  SKIPPED: '已跳过',
};
const terminal = (status: RunStatus) => !['PENDING', 'RUNNING', 'RETRY_WAIT'].includes(status);

function exampleValue(schema: Schema): JsonValue {
  if (schema.default !== undefined) return schema.default;
  if (schema.enum?.length) return schema.enum[0];
  switch (schema.type) {
    case 'object':
      return Object.fromEntries(
        Object.entries(schema.properties ?? {}).map(([key, value]) => [key, exampleValue(value)]),
      );
    case 'array':
      return [];
    case 'boolean':
      return false;
    case 'integer':
    case 'number':
      return typeof schema.minimum === 'number' ? schema.minimum : 0;
    default:
      return '';
  }
}

function exampleInput(definition: Definition): string {
  let value = exampleValue(definition.inputSchema);
  if (definition.workflowId === 'text-demo') value = { text: ' Hello ' };
  if (definition.workflowId === 'order-investigation') value = { orderId: 'demo-1001' };
  return JSON.stringify(value, null, 2);
}

function inputObject(text: string): Record<string, JsonValue> {
  const value: unknown = JSON.parse(text);
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('测试输入必须是 JSON 对象。');
  return value as Record<string, JsonValue>;
}

function milliseconds(start?: string, end?: string): string {
  if (!start || !end) return '—';
  const elapsed = new Date(end).getTime() - new Date(start).getTime();
  return Number.isFinite(elapsed) ? `${Math.max(0, elapsed)} ms` : '—';
}

function validateSnapshot(snapshot: RunSnapshot): RunSnapshot {
  if (
    !snapshot ||
    typeof snapshot.runId !== 'string' ||
    !Object.hasOwn(labels, snapshot.status) ||
    !Array.isArray(snapshot.nodes)
  ) {
    throw new Error('服务返回了无效的运行记录。');
  }
  return snapshot;
}

interface SubmittedRun {
  revision: number;
  input: string;
}

export default function RunPanel({
  definition,
  revision,
  disabled = false,
}: {
  definition: Definition;
  revision: number;
  disabled?: boolean;
}) {
  const [input, setInput] = useState(() => exampleInput(definition));
  const [snapshot, setSnapshot] = useState<RunSnapshot | null>(null);
  const [submitted, setSubmitted] = useState<SubmittedRun | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [queryError, setQueryError] = useState<string | null>(null);
  const [queryVersion, setQueryVersion] = useState(0);
  const controllerRef = useRef<AbortController | null>(null);
  const inFlightRef = useRef(false);
  useEffect(() => () => controllerRef.current?.abort(), []);

  const active = snapshot !== null && !terminal(snapshot.status);
  const polledRunID = active ? snapshot.runId : null;
  useEffect(() => {
    if (!polledRunID) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const next = validateSnapshot(
          await fetchData<RunSnapshot>(`/api/v1/runs/${encodeURIComponent(polledRunID)}`, controller.signal),
        );
        if (controller.signal.aborted) return;
        if (next.runId !== polledRunID) throw new Error('运行记录与当前请求不一致。');
        setSnapshot(next);
        setQueryError(null);
        if (!terminal(next.status)) timer = setTimeout(poll, 500);
      } catch (failure) {
        if (!controller.signal.aborted) {
          if (failure instanceof APIError && failure.status === 404) {
            setError(`运行 ${polledRunID} 的记录已不可用，可能已被清理或服务已重启。`);
            setSnapshot(null);
          } else setQueryError(errorMessage(failure));
        }
      }
    };
    timer = setTimeout(poll, 250);
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [polledRunID, queryVersion]);

  const start = async () => {
    if (disabled || inFlightRef.current || active) return;
    try {
      inputObject(input);
    } catch (failure) {
      setError(errorMessage(failure));
      return;
    }
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    inFlightRef.current = true;
    setSubmitting(true);
    setError(null);
    setQueryError(null);
    setSnapshot(null);
    const submittedDefinition = structuredClone(definition);
    const submittedRevision = revision;
    const submittedInput = input;
    try {
      const result = validateSnapshot(
        await postJSONData<RunSnapshot>(
          '/api/v1/test-runs',
          `{"definition":${JSON.stringify(submittedDefinition)},"input":${submittedInput}}`,
          controller.signal,
        ),
      );
      if (controller.signal.aborted) return;
      setSubmitted({ revision: submittedRevision, input: submittedInput });
      setSnapshot(result);
    } catch (failure) {
      if (!controller.signal.aborted)
        setError(
          `${errorMessage(failure)}${failure instanceof APIError && failure.status === 0 ? ' 提交结果不明时，请勿直接重复提交。' : ''}`,
        );
    } finally {
      if (!controller.signal.aborted) setSubmitting(false);
      inFlightRef.current = false;
    }
  };

  return (
    <section className="run-panel" aria-label="草稿测试运行">
      <div className="run-panel-heading">
        <div>
          <span className="eyebrow">TEST RUN</span>
          <h2>测试运行</h2>
        </div>
        <span className="tag">草稿 r{revision}</span>
      </div>
      <p className="muted">试跑将真实执行当前草稿中的已注册节点。结果保存在当前服务进程中。</p>
      <label className="run-input-label" htmlFor="test-run-input">
        测试输入 JSON
      </label>
      <textarea
        id="test-run-input"
        className="run-input"
        spellCheck={false}
        value={input}
        onChange={(event) => setInput(event.target.value)}
        disabled={submitting || active}
      />
      <div className="run-actions">
        <button
          className="button run-primary"
          onClick={() => void start()}
          disabled={disabled || submitting || active}
        >
          {submitting ? '提交中…' : active ? '运行中…' : '执行测试'}
        </button>
        <button
          className="button subtle"
          onClick={() => {
            setInput(exampleInput(definition));
            setError(null);
          }}
          disabled={submitting || active}
        >
          载入示例输入
        </button>
        {disabled && <span className="form-error">请先修正编辑表单中的错误。</span>}
      </div>
      {error && (
        <p role="alert" className="run-error">
          {error}
        </p>
      )}
      {snapshot && (
        <div className="run-result" data-testid="run-result">
          <div className="run-result-heading">
            <h3>运行结果</h3>
            <span className={`run-status run-${snapshot.status.toLowerCase()}`}>
              {labels[snapshot.status]}
            </span>
          </div>
          <p className="subtle-note">
            运行 ID：<code>{snapshot.runId}</code> · 草稿 r{submitted?.revision} ·{' '}
            {milliseconds(snapshot.startedAt, snapshot.finishedAt)}
          </p>
          {submitted?.revision !== revision && (
            <p className="run-stale">当前草稿已变更。以下结果对应提交时的草稿 r{submitted?.revision}。</p>
          )}
          {queryError && (
            <div role="alert" className="run-error">
              状态查询失败：{queryError}
              <button
                className="text-button"
                onClick={() => {
                  setQueryError(null);
                  setQueryVersion((value) => value + 1);
                }}
              >
                重试查询
              </button>
              <p>查询失败不代表执行失败，请先恢复查询。</p>
            </div>
          )}
          {snapshot.error && (
            <p role="alert" className="run-error">
              {snapshot.error.nodeId && `${snapshot.error.nodeId}：`}
              {snapshot.error.message} · {snapshot.error.code}
            </p>
          )}
          <div className="run-table-scroll">
            <table className="run-table">
              <thead>
                <tr>
                  <th>节点</th>
                  <th>状态</th>
                  <th>尝试</th>
                  <th>耗时</th>
                </tr>
              </thead>
              <tbody>
                {snapshot.nodes.map((node) => (
                  <tr key={node.nodeId}>
                    <td>
                      <code>{node.nodeId}</code>
                      {node.error && <small className="run-error">{node.error.code}</small>}
                    </td>
                    <td>
                      <span className={`run-status run-${node.status.toLowerCase()}`}>
                        {labels[node.status] ?? node.status}
                      </span>
                    </td>
                    <td>
                      {node.attempt}
                      {node.attempts.length > 0 && (
                        <details>
                          <summary>记录</summary>
                          {node.attempts.map((attempt) => (
                            <p key={attempt.attempt}>
                              #{attempt.attempt} {labels[attempt.status]} ·{' '}
                              {milliseconds(attempt.startedAt, attempt.finishedAt)}
                              {attempt.error && ` · ${attempt.error.code}`}
                            </p>
                          ))}
                        </details>
                      )}
                    </td>
                    <td>{milliseconds(node.startedAt, node.finishedAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {snapshot.status === 'SUCCEEDED' && (
            <div data-testid="run-output">
              <h3>流程输出</h3>
              {hasUnsafeNumber(snapshot.output) ? (
                <p className="run-error">
                  输出含超出浏览器精度的数值，请通过运行查询 API 读取原始 JSON：
                  <code>/api/v1/runs/{snapshot.runId}</code>
                </p>
              ) : (
                <JsonBlock value={snapshot.output ?? {}} />
              )}
            </div>
          )}
          <details>
            <summary>本次提交的输入</summary>
            <pre className="json-block">{submitted?.input ?? '{}'}</pre>
          </details>
        </div>
      )}
    </section>
  );
}
