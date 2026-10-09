import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { WorkflowConsole } from './index';
import { descriptor, workflowFixture } from './test/fixtures';
import type { WorkflowView } from '@/api/workflow.types';

// Graph layout is covered independently; the browser suite exercises the actual canvas.
vi.mock('./WorkflowCanvas', () => ({
  default: ({ view, onSelect }: { view: WorkflowView; onSelect: (id: string) => void }) => (
    <div aria-label="只读流程画布">
      {view.nodes.map((node) => (
        <button key={node.id} onClick={() => onSelect(node.id)}>
          查看节点 {node.id}
        </button>
      ))}
    </div>
  ),
}));

function response(data: unknown, status = 200) {
  return new Response(JSON.stringify(status === 200 ? { data } : { error: data }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function mockService(view = workflowFixture()) {
  return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const path = String(input);
    if (path === '/api/v1/workflows')
      return response({
        items: [
          {
            workflowId: view.workflowId,
            definitionVersion: view.definitionVersion,
            title: view.title,
            nodeCount: view.nodes.length,
          },
        ],
      });
    if (path === '/api/v1/node-types') return response({ items: [descriptor] });
    return response(view);
  });
}

afterEach(() => vi.restoreAllMocks());

describe('developer console', () => {
  it('reads an exact version and displays source bindings plus effective policies for selected instances', async () => {
    const fetchMock = mockService();
    render(<WorkflowConsole />);
    fireEvent.click(await screen.findByRole('button', { name: '查看节点 second' }));
    const details = within(screen.getByRole('complementary', { name: '节点详情' }));
    expect(details.getByText('second')).toBeInTheDocument();
    expect(details.getByText('first.text')).toBeInTheDocument();
    expect(details.getByText('2,000 ms')).toBeInTheDocument();
    expect(details.getByText('2 次')).toBeInTheDocument();
    expect(details.getByText('100 ms')).toBeInTheDocument();
    fireEvent.click(details.getByRole('button', { name: 'first.text' }));
    expect(within(screen.getByTestId('node-detail')).getByText('first')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/workflows/demo/versions/1', expect.any(Object));
    expect(screen.queryByRole('button', { name: /^运行$/ })).not.toBeInTheDocument();
  });

  it('shows API failures with a retry action and handles a missing version', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new TypeError('connection failed'));
    render(<WorkflowConsole />);
    expect(await screen.findByText('无法读取流程列表')).toBeInTheDocument();
    fetchMock.mockImplementation(async (input) =>
      String(input) === '/api/v1/workflows'
        ? response({
            items: [{ workflowId: 'demo', definitionVersion: '7', title: '演示流程', nodeCount: 2 }],
          })
        : response({ code: 'WORKFLOW_NOT_FOUND', message: '指定版本不存在' }, 404),
    );
    fireEvent.click(screen.getByRole('button', { name: '重新加载' }));
    expect(await screen.findByText('无法展示流程')).toBeInTheDocument();
    expect(screen.getByText(/指定版本不存在.*WORKFLOW_NOT_FOUND/)).toBeInTheDocument();
  });

  it('surfaces invalid graphs without silently omitting malformed nodes', async () => {
    const view = workflowFixture();
    view.edges[0].target = 'missing';
    mockService(view);
    render(<WorkflowConsole />);
    expect(await screen.findByText(/流程数据异常：连线重复或引用不存在的节点/)).toBeInTheDocument();
    expect(screen.queryByLabelText('只读流程画布')).not.toBeInTheDocument();
  });

  it('supports generic catalog search and renders HTML-like metadata as text', async () => {
    const view = workflowFixture();
    view.title = '<img src=x onerror=alert(1)>';
    mockService(view);
    render(<WorkflowConsole />);
    expect(await screen.findByRole('heading', { name: view.title })).toBeInTheDocument();
    expect(document.querySelector('img')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '节点目录' }));
    const input = await screen.findByRole('searchbox', { name: '搜索节点' });
    fireEvent.change(input, { target: { value: 'demo.echo' } });
    expect(screen.getByRole('complementary', { name: '节点类型详情' })).toBeInTheDocument();
    fireEvent.change(input, { target: { value: 'no matching node' } });
    expect(screen.getByText('没有匹配的节点')).toBeInTheDocument();
  });

  it('shows an empty workflow list and clear setup guidance', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(response({ items: [] }));
    render(<WorkflowConsole />);
    expect(await screen.findByText('暂无已加载流程')).toBeInTheDocument();
    expect(screen.getByText(/将有效的 YAML 配置放入流程目录/)).toBeInTheDocument();
  });

  it('does not replace a newly selected version with a stale response', async () => {
    let completeFirst: ((response: Response) => void) | undefined;
    const base = workflowFixture();
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const path = String(input);
      if (path === '/api/v1/workflows')
        return response({
          items: ['1', '2'].map((version) => ({
            workflowId: 'demo',
            definitionVersion: version,
            title: `流程版本 ${version}`,
            nodeCount: 2,
          })),
        });
      if (path.endsWith('/1'))
        return new Promise<Response>((resolve) => {
          completeFirst = resolve;
        });
      return response({ ...base, definitionVersion: '2' });
    });
    render(<WorkflowConsole />);
    await waitFor(() => expect(completeFirst).toBeDefined());
    fireEvent.click(screen.getByRole('button', { name: /流程版本 2/ }));
    await screen.findByRole('button', { name: '查看节点 first' });
    completeFirst!(response(base));
    await waitFor(() => expect(screen.getByRole('heading', { name: '流程版本 2' })).toBeInTheDocument());
    expect(screen.queryByText('服务返回的流程版本与所选版本不一致。')).not.toBeInTheDocument();
  });
});
