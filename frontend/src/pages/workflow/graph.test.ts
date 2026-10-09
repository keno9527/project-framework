import { describe, expect, it } from 'vitest';
import { layoutWorkflow, validateWorkflow } from './graph';
import { workflowFixture } from './test/fixtures';

describe('workflow graph contract', () => {
  it('keeps same-type instances distinct and lays out directed dependencies', () => {
    const graph = layoutWorkflow(workflowFixture(), 'second', () => {});
    expect(graph.nodes.map((node) => node.id)).toEqual(['first', 'second']);
    expect(graph.nodes[0].position.x).toBeLessThan(graph.nodes[1].position.x);
    expect(graph.nodes[0].selected).toBe(false);
    expect(graph.nodes[1].selected).toBe(true);
    expect(graph.nodes.every((node) => !node.draggable && !node.connectable && !node.deletable)).toBe(true);
    expect(graph.edges[0].markerEnd).toBeTruthy();
  });

  it.each([
    'duplicate',
    'dangling',
    'cycle',
    'missing-descriptor',
    'missing-edge',
    'bad-input',
    'missing-policy',
  ])('rejects malformed graphs rather than dropping nodes (%s)', (problem) => {
    const view = workflowFixture();
    if (problem === 'duplicate') view.nodes.push(view.nodes[0]);
    if (problem === 'dangling') view.edges[0].target = 'absent';
    if (problem === 'cycle') view.edges.push({ id: 'back', source: 'second', target: 'first' });
    if (problem === 'missing-descriptor') view.nodeTypes = [];
    if (problem === 'missing-edge') view.edges = [];
    if (problem === 'bad-input') view.nodes[0].inputs = { text: { kind: 'workflowInput', field: 'absent' } };
    if (problem === 'missing-policy') view.nodes[0].timeoutMs = Number.NaN;
    expect(() => validateWorkflow(view)).toThrow('流程数据异常');
  });

  it('accepts absent optional config and input maps', () => {
    const view = workflowFixture();
    delete view.nodes[0].config;
    delete view.nodes[0].inputs;
    expect(() => validateWorkflow(view)).not.toThrow();
  });

  it('layouts the documented 50 node / 200 edge bound without missing or invalid coordinates', () => {
    const view = workflowFixture();
    view.nodes = Array.from({ length: 50 }, (_, index) => ({
      ...view.nodes[0],
      id: `node-${index}`,
      inputs: {},
      dependsOn: [],
    }));
    view.edges = [];
    view.outputs = {};
    for (let target = 1; target < 50; target++) {
      for (let source = Math.max(0, target - 5); source < target && view.edges.length < 200; source++) {
        const sourceID = `node-${source}`,
          targetID = `node-${target}`;
        view.nodes[target].dependsOn!.push(sourceID);
        view.edges.push({
          id: `${sourceID}-${targetID}`,
          source: sourceID,
          target: targetID,
          reasons: ['explicit'],
        });
      }
    }
    const graph = layoutWorkflow(view, null, () => {});
    expect(graph.nodes).toHaveLength(50);
    expect(graph.edges).toHaveLength(200);
    expect(
      graph.nodes.every(({ position }) => Number.isFinite(position.x) && Number.isFinite(position.y)),
    ).toBe(true);
  });
});
