import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import DraftWorkspace from './DraftWorkspace';
import { definitionFromView, draftStorageKey } from './draft';
import { descriptor, workflowFixture } from './test/fixtures';
import type { WorkflowView } from '@/api/workflow.types';

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
vi.mock('./RunPanel', () => ({
  default: ({ disabled }: { disabled?: boolean }) => <button disabled={disabled}>执行测试</button>,
}));

const response = (data: unknown, status = 200) =>
  new Response(JSON.stringify(status === 200 ? { data } : { error: data }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe('draft editing workspace', () => {
  it('edits schema-driven config, retains the loaded graph until validation, and restores a saved draft', async () => {
    const view = workflowFixture();
    view.nodeTypes[0] = {
      ...descriptor,
      configSchema: { type: 'object', properties: { trim: { type: 'boolean' } }, required: ['trim'] },
    };
    view.nodes.forEach((node) => {
      node.config = { trim: true };
    });
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_, init) => {
      if (init?.method === 'POST') {
        const { definition } = JSON.parse(init.body as string);
        return response({ view: { ...view, ...definition }, yaml: 'apiVersion: workflow/v1\n' });
      }
      return response({ items: view.nodeTypes });
    });
    const mounted = render(<DraftWorkspace view={view} />);
    fireEvent.click(screen.getByRole('button', { name: '编辑节点' }));
    fireEvent.click(screen.getByRole('button', { name: '编辑节点 first' }));
    fireEvent.click(await screen.findByRole('checkbox', { name: '配置 trim' }));
    expect(screen.getByText('上次有效预览 · 草稿待校验')).toBeInTheDocument();
    expect(JSON.parse(localStorage.getItem(draftStorageKey(view))!).definition.nodes[0].config.trim).toBe(
      false,
    );
    fireEvent.click(screen.getByRole('button', { name: '校验草稿' }));
    expect(await screen.findByText('草稿已校验')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/workflows/validate',
      expect.objectContaining({ method: 'POST' }),
    );
    mounted.unmount();
    render(<DraftWorkspace view={view} />);
    expect(screen.getByText(/已恢复此浏览器保存的草稿/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '编辑节点 first' }));
    expect(screen.getByRole('checkbox', { name: '配置 trim' })).not.toBeChecked();
  });

  it('blocks deleting referenced nodes and allows a newly added unused node to be removed', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(response({ items: [descriptor] }));
    render(<DraftWorkspace view={workflowFixture()} />);
    fireEvent.click(screen.getByRole('button', { name: '编辑节点' }));
    fireEvent.click(screen.getByRole('button', { name: '编辑节点 first' }));
    expect(screen.getByRole('button', { name: '删除节点' })).toBeDisabled();
    expect(screen.getByText(/先修改以下引用后才能删除：second.text/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: '＋ 添加节点' }));
    fireEvent.change(screen.getByRole('textbox', { name: '新节点 ID' }), {
      target: { value: '1_invalid.name' },
    });
    fireEvent.click(screen.getByRole('button', { name: '确认添加' }));
    expect(screen.getByText('节点 ID 需以字母开头，只能包含字母、数字和连字符。')).toBeInTheDocument();
    fireEvent.change(screen.getByRole('textbox', { name: '新节点 ID' }), { target: { value: 'third' } });
    fireEvent.click(screen.getByRole('button', { name: '确认添加' }));
    await screen.findByRole('button', { name: '编辑节点 third' });
    expect(screen.getByRole('button', { name: '删除节点' })).toBeEnabled();
    fireEvent.click(screen.getByRole('button', { name: '删除节点' }));
    expect(screen.queryByRole('button', { name: '编辑节点 third' })).not.toBeInTheDocument();
  });

  it('keeps failed changes editable and ignores validation responses for an older revision', async () => {
    const view = workflowFixture();
    let resolveValidation: ((response: Response) => void) | undefined;
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (_, init) =>
      init?.method === 'POST'
        ? new Promise<Response>((resolve) => {
            resolveValidation = resolve;
          })
        : response({ items: [descriptor] }),
    );
    render(<DraftWorkspace view={view} />);
    fireEvent.click(screen.getByRole('button', { name: '编辑节点' }));
    const title = screen.getByRole('textbox', { name: '流程标题' });
    fireEvent.change(title, { target: { value: 'First change' } });
    fireEvent.click(screen.getByRole('button', { name: '校验草稿' }));
    await waitFor(() => expect(resolveValidation).toBeDefined());
    fireEvent.change(title, { target: { value: 'Latest change' } });
    await act(async () =>
      resolveValidation!(response({ view: { ...view, title: 'First change' }, yaml: 'draft' })),
    );
    expect(title).toHaveValue('Latest change');
    expect(screen.getByText('上次有效预览 · 草稿待校验')).toBeInTheDocument();
    expect(screen.queryByText('草稿已校验')).not.toBeInTheDocument();
  });

  it('prevents validation and running while a nested JSON field contains invalid text', async () => {
    const view = workflowFixture();
    view.nodeTypes[0] = {
      ...descriptor,
      configSchema: { type: 'object', properties: { options: { type: 'object' } }, required: ['options'] },
    };
    view.nodes[0].config = { options: {} };
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(response({ items: view.nodeTypes }));
    render(<DraftWorkspace view={view} />);
    fireEvent.click(screen.getByRole('button', { name: '编辑节点' }));
    fireEvent.click(screen.getByRole('button', { name: '编辑节点 first' }));
    const textarea = await screen.findByRole('textbox', { name: '配置 options（JSON）' });
    fireEvent.change(textarea, { target: { value: '{' } });
    expect(screen.getByRole('button', { name: '校验草稿' })).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: '▶ 测试运行' }));
    expect(screen.getByRole('button', { name: '执行测试' })).toBeDisabled();
    fireEvent.change(textarea, { target: { value: '{"enabled":true}' } });
    expect(screen.getByRole('button', { name: '校验草稿' })).toBeEnabled();
    expect(screen.getByRole('button', { name: '执行测试' })).toBeEnabled();
  });

  it('requires explicit draft discard and clears saved data when restored', async () => {
    const view = workflowFixture();
    localStorage.setItem(
      draftStorageKey(view),
      JSON.stringify({ format: 1, definition: { ...definitionFromView(view), title: 'saved' } }),
    );
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(response({ items: [descriptor] }));
    render(<DraftWorkspace view={view} />);
    expect(screen.getByRole('textbox', { name: '流程标题' })).toHaveValue('saved');
    fireEvent.click(screen.getByRole('button', { name: '恢复加载版本' }));
    const confirm = within(screen.getByRole('alert'));
    fireEvent.click(confirm.getByRole('button', { name: '丢弃草稿并恢复' }));
    expect(screen.getByRole('textbox', { name: '流程标题' })).toHaveValue(view.title);
    expect(localStorage.getItem(draftStorageKey(view))).toBeNull();
    await waitFor(() => expect(screen.getByText('已加载版本')).toBeInTheDocument());
  });
});
