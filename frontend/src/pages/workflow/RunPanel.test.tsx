import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import RunPanel from './RunPanel';
import { workflowFixture } from './test/fixtures';
import type { Definition, RunSnapshot } from '@/api/workflow.types';

function definition(): Definition {
  const view = workflowFixture();
  return {
    apiVersion: view.apiVersion,
    workflowId: view.workflowId,
    definitionVersion: view.definitionVersion,
    title: view.title,
    inputSchema: view.inputSchema,
    outputSchema: view.outputSchema,
    outputs: view.outputs,
    nodes: view.nodes,
  };
}

function snapshot(status: RunSnapshot['status'] = 'SUCCEEDED'): RunSnapshot {
  return {
    runId: 'test-run-1',
    workflowId: 'demo',
    definitionVersion: '1',
    status,
    instanceId: 'instance1',
    ephemeral: true,
    createdAt: '2026-09-20T00:00:00Z',
    startedAt: '2026-09-20T00:00:00Z',
    finishedAt: status === 'SUCCEEDED' ? '2026-09-20T00:00:00.020Z' : undefined,
    output: status === 'SUCCEEDED' ? { text: 'draft output' } : undefined,
    nodes: [
      {
        nodeId: 'first',
        nodeExecutionId: 'execution1',
        status,
        attempt: 1,
        attempts: [{ attempt: 1, status, startedAt: '2026-09-20T00:00:00Z' }],
      },
    ],
  };
}

function response(data: unknown, status = 200) {
  return new Response(JSON.stringify(status < 400 ? { data } : { error: data }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('draft test runner', () => {
  it('preserves authored numeric tokens and duplicate keys for backend validation', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(response({ code: 'INVALID_REQUEST', message: 'duplicate key' }, 400));
    render(<RunPanel definition={definition()} revision={0} />);
    const rawInput = '{"count":9007199254740993,"small":1e-400,"text":"a","text":"b"}';
    fireEvent.change(screen.getByLabelText('测试输入 JSON'), { target: { value: rawInput } });
    fireEvent.click(screen.getByRole('button', { name: '执行测试' }));
    await screen.findByRole('alert');
    expect(fetchMock.mock.calls[0][1]?.body).toContain(`"input":${rawInput}`);
  });

  it('submits the edited definition once and shows real output with the submitted revision', async () => {
    let finish: ((value: Response) => void) | undefined;
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const draft = definition();
    draft.nodes[0].title = 'Edited node';
    const { rerender } = render(<RunPanel definition={draft} revision={3} />);
    fireEvent.change(screen.getByLabelText('测试输入 JSON'), { target: { value: '{"text":"custom"}' } });
    fireEvent.click(screen.getByRole('button', { name: '执行测试' }));
    expect(screen.getByRole('button', { name: '提交中…' })).toBeDisabled();
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/v1/test-runs');
    expect(JSON.parse(String(options?.body))).toEqual({ definition: draft, input: { text: 'custom' } });
    finish!(response(snapshot(), 202));
    expect(await screen.findByTestId('run-output')).toHaveTextContent('draft output');
    rerender(<RunPanel definition={draft} revision={4} />);
    expect(screen.getByText(/以下结果对应提交时的草稿 r3/)).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('rejects non-object JSON locally and respects invalid-editor state', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch');
    const { rerender } = render(<RunPanel definition={definition()} revision={0} />);
    fireEvent.change(screen.getByLabelText('测试输入 JSON'), { target: { value: '[]' } });
    fireEvent.click(screen.getByRole('button', { name: '执行测试' }));
    expect(screen.getByRole('alert')).toHaveTextContent('必须是 JSON 对象');
    expect(fetchMock).not.toHaveBeenCalled();
    rerender(<RunPanel definition={definition()} revision={1} disabled />);
    expect(screen.getByRole('button', { name: '执行测试' })).toBeDisabled();
  });

  it('retries a failed GET without resubmitting the accepted run', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(response(snapshot('RUNNING'), 202))
      .mockRejectedValueOnce(new TypeError('offline'))
      .mockResolvedValueOnce(response(snapshot()));
    render(<RunPanel definition={definition()} revision={0} />);
    fireEvent.click(screen.getByRole('button', { name: '执行测试' }));
    expect(await screen.findByRole('button', { name: '重试查询' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '运行中…' })).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: '重试查询' }));
    await screen.findByTestId('run-output');
    expect(fetchMock.mock.calls.filter(([, options]) => options?.method === 'POST')).toHaveLength(1);
    expect(fetchMock.mock.calls.filter(([url]) => url === '/api/v1/runs/test-run-1')).toHaveLength(2);
  });

  it('shows compilation failures without inventing a run result', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(
        response(
          { code: 'DEFINITION_INVALID', message: 'node first.inputs: required binding is missing' },
          422,
        ),
      );
    render(<RunPanel definition={definition()} revision={0} />);
    fireEvent.click(screen.getByRole('button', { name: '执行测试' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('DEFINITION_INVALID');
    expect(screen.queryByTestId('run-result')).not.toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('unblocks a new test if the ephemeral record is gone', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(response(snapshot('RUNNING'), 202))
      .mockResolvedValueOnce(response({ code: 'RUN_NOT_FOUND', message: 'not found' }, 404));
    render(<RunPanel definition={definition()} revision={0} />);
    fireEvent.click(screen.getByRole('button', { name: '执行测试' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('test-run-1 的记录已不可用'));
    expect(screen.getByRole('button', { name: '执行测试' })).toBeEnabled();
  });
});
