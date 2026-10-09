import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  assertSafeNumbers,
  availableOutputs,
  createNode,
  definitionFromView,
  draftStorageKey,
  initialValue,
  nodeReferences,
  readDraft,
  saveDraft,
  withBinding,
} from './draft';
import { descriptor, workflowFixture } from './test/fixtures';

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe('editable workflow drafts', () => {
  it('strips graph projection and snapshots independently of the loaded definition', () => {
    const view = workflowFixture();
    const draft = definitionFromView(view);
    expect(draft).not.toHaveProperty('edges');
    expect(draft).not.toHaveProperty('nodeTypes');
    draft.nodes[0].title = 'Changed';
    expect(view.nodes[0].title).toBeUndefined();
  });

  it('isolates storage by workflow and version and recovers a parseable draft', () => {
    const view = workflowFixture();
    const draft = definitionFromView(view);
    draft.nodes[0].title = '本地修改';
    expect(saveDraft(draft)).toBeUndefined();
    expect(readDraft(view)).toMatchObject({ restored: true, definition: draft });
    expect(readDraft({ ...view, definitionVersion: '2' }).restored).toBe(false);
    expect(readDraft({ ...view, workflowId: 'another' }).restored).toBe(false);
  });

  it('rejects corrupted nested bindings and duplicated IDs without crashing the editor', () => {
    const view = workflowFixture();
    const draft = definitionFromView(view);
    localStorage.setItem(
      draftStorageKey(view),
      JSON.stringify({
        format: 1,
        definition: {
          ...draft,
          outputs: { text: null },
        },
      }),
    );
    expect(readDraft(view)).toMatchObject({ restored: false, notice: expect.stringContaining('格式已失效') });
    localStorage.setItem(
      draftStorageKey(view),
      JSON.stringify({
        format: 1,
        definition: {
          ...draft,
          nodes: [draft.nodes[0], draft.nodes[0]],
        },
      }),
    );
    expect(readDraft(view).restored).toBe(false);
  });

  it('reports unavailable local storage rather than promising a saved draft', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('Full');
    });
    expect(saveDraft(definitionFromView(workflowFixture()))).toContain('无法保存');
  });

  it('protects data, explicit dependency, and final output references when deleting a node', () => {
    const draft = definitionFromView(workflowFixture());
    draft.nodes[1].dependsOn = ['first'];
    expect(nodeReferences(draft, 'first')).toEqual(['second.text', 'second 的顺序依赖']);
    expect(nodeReferences(draft, 'second')).toEqual(['流程输出 text']);
  });

  it('enumerates registered outputs without offering self-dependency and applies binding removal immutably', () => {
    const draft = definitionFromView(workflowFixture());
    expect(availableOutputs(draft, [descriptor], 'first')).toEqual([
      { nodeId: 'second', field: 'text', schema: { type: 'string' } },
    ]);
    const original = draft.nodes[0].inputs;
    expect(withBinding(original, 'text', undefined)).toEqual({});
    expect(original).toHaveProperty('text');
  });

  it('creates explicit editable values without applying schema default annotations', () => {
    expect(initialValue({ type: 'boolean', default: true })).toBe(false);
    expect(
      initialValue({
        type: 'object',
        required: ['enabled'],
        properties: {
          enabled: { type: 'boolean' },
          optional: { type: 'string', default: 'annotation' },
        },
      }),
    ).toEqual({ enabled: false });
    expect(createNode(descriptor, 'third')).toMatchObject({
      id: 'third',
      config: {},
      inputs: {},
      retry: { maxAttempts: 1 },
    });
  });

  it('prevents silently saving rounded integers or non-finite numbers', () => {
    expect(() => assertSafeNumbers({ values: [1, { large: 9007199254740992 }] })).toThrow('浏览器精度');
    expect(() => assertSafeNumbers({ bound: Infinity })).toThrow('浏览器精度');
    expect(() => assertSafeNumbers({ integer: 9007199254740991, fraction: 1.25 })).not.toThrow();
    const draft = definitionFromView(workflowFixture());
    draft.nodes[0].config = { tooLarge: 9007199254740992 };
    expect(saveDraft(draft)).toContain('浏览器精度');
    expect(localStorage.getItem(draftStorageKey(draft))).toBeNull();
  });
});
