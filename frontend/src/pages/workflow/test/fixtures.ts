import type { NodeDescriptor, Schema, WorkflowView } from '@/api/workflow.types';

const textSchema: Schema = {
  type: 'object',
  properties: { text: { type: 'string' } },
  required: ['text'],
  additionalProperties: false,
};
export const descriptor: NodeDescriptor = {
  type: 'demo.echo',
  typeVersion: '1',
  title: '回显文本',
  description: '将输入文本原样返回。',
  category: '演示',
  configSchema: { type: 'object', properties: {}, additionalProperties: false },
  inputSchema: textSchema,
  outputSchema: textSchema,
  retrySafe: true,
};

// Deliberately uses two instances of one generic type, independent of builtin nodes.
export function workflowFixture(): WorkflowView {
  return {
    apiVersion: 'workflow/v1',
    workflowId: 'demo',
    definitionVersion: '1',
    title: '演示流程',
    inputSchema: textSchema,
    outputSchema: textSchema,
    outputs: { text: { kind: 'nodeOutput', nodeId: 'second', field: 'text' } },
    nodes: [
      {
        id: 'first',
        type: 'demo.echo',
        typeVersion: '1',
        config: {},
        inputs: { text: { kind: 'workflowInput', field: 'text' } },
        timeoutMs: 30000,
        retry: { maxAttempts: 1, backoffMs: 200 },
      },
      {
        id: 'second',
        type: 'demo.echo',
        typeVersion: '1',
        config: {},
        inputs: { text: { kind: 'nodeOutput', nodeId: 'first', field: 'text' } },
        timeoutMs: 2000,
        retry: { maxAttempts: 2, backoffMs: 100 },
      },
    ],
    edges: [
      {
        id: 'first-second',
        source: 'first',
        target: 'second',
        reasons: ['data'],
        mappings: [{ sourceField: 'text', targetField: 'text' }],
      },
    ],
    nodeTypes: [descriptor],
  };
}
