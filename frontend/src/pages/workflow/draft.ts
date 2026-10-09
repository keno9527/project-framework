import {
  typeKey,
  type Binding,
  type Definition,
  type JsonValue,
  type NodeDescriptor,
  type Schema,
  type WorkflowNode,
  type WorkflowView,
} from '@/api/workflow.types';

export const draftStorageKey = (definition: Pick<Definition, 'workflowId' | 'definitionVersion'>) =>
  `workflow-console:draft:v1:${JSON.stringify([definition.workflowId, definition.definitionVersion])}`;

export const UNSAFE_NUMBER_MESSAGE = '大整数或非有限数字超出浏览器精度，不能安全编辑；请使用原 YAML / API。';

export function hasUnsafeNumber(value: unknown): boolean {
  if (typeof value === 'number')
    return !Number.isFinite(value) || (Number.isInteger(value) && !Number.isSafeInteger(value));
  if (Array.isArray(value)) return value.some(hasUnsafeNumber);
  if (value && typeof value === 'object') return Object.values(value).some(hasUnsafeNumber);
  return false;
}

export function assertSafeNumbers(value: unknown): void {
  if (hasUnsafeNumber(value)) throw new Error(UNSAFE_NUMBER_MESSAGE);
}

function record(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function validBindings(value: unknown): boolean {
  if (value === undefined) return true;
  if (!record(value)) return false;
  return Object.values(value).every(
    (binding) =>
      record(binding) &&
      ((binding.kind === 'literal' && Object.hasOwn(binding, 'value')) ||
        (binding.kind === 'workflowInput' && typeof binding.field === 'string') ||
        (binding.kind === 'nodeOutput' &&
          typeof binding.nodeId === 'string' &&
          typeof binding.field === 'string')),
  );
}

function validSchemaShape(schema: unknown): schema is Schema {
  if (!record(schema) || typeof schema.type !== 'string') return false;
  if (
    schema.properties !== undefined &&
    (!record(schema.properties) || !Object.values(schema.properties).every(validSchemaShape))
  )
    return false;
  if (
    schema.required !== undefined &&
    (!Array.isArray(schema.required) || !schema.required.every((field) => typeof field === 'string'))
  )
    return false;
  if (schema.items !== undefined && !validSchemaShape(schema.items)) return false;
  return schema.enum === undefined || Array.isArray(schema.enum);
}

// Copy only editable definition fields. Compiled edges and node descriptors never enter a draft.
export function definitionFromView(view: WorkflowView): Definition {
  return structuredClone({
    apiVersion: view.apiVersion,
    workflowId: view.workflowId,
    definitionVersion: view.definitionVersion,
    title: view.title,
    inputSchema: view.inputSchema,
    outputSchema: view.outputSchema,
    nodes: view.nodes,
    outputs: view.outputs ?? {},
  });
}

export function readDraft(view: WorkflowView): {
  definition: Definition;
  restored: boolean;
  notice?: string;
} {
  const original = definitionFromView(view);
  try {
    const saved = localStorage.getItem(draftStorageKey(view));
    if (!saved) return { definition: original, restored: false };
    const parsed = JSON.parse(saved) as { format?: number; definition?: Definition };
    const draft = parsed.definition;
    if (
      parsed.format !== 1 ||
      !draft ||
      draft.apiVersion !== 'workflow/v1' ||
      draft.workflowId !== view.workflowId ||
      draft.definitionVersion !== view.definitionVersion ||
      typeof draft.title !== 'string' ||
      !Array.isArray(draft.nodes) ||
      !validSchemaShape(draft.inputSchema) ||
      !validSchemaShape(draft.outputSchema) ||
      !validBindings(draft.outputs) ||
      draft.nodes.length === 0 ||
      draft.nodes.length > 50 ||
      new Set(draft.nodes.map((node) => node?.id)).size !== draft.nodes.length ||
      draft.nodes.some(
        (node) =>
          !node ||
          typeof node.id !== 'string' ||
          !node.retry ||
          typeof node.type !== 'string' ||
          typeof node.typeVersion !== 'string' ||
          (node.title !== undefined && typeof node.title !== 'string') ||
          typeof node.timeoutMs !== 'number' ||
          typeof node.retry.maxAttempts !== 'number' ||
          typeof node.retry.backoffMs !== 'number' ||
          (node.config !== undefined && !record(node.config)) ||
          !validBindings(node.inputs) ||
          (node.dependsOn !== undefined &&
            (!Array.isArray(node.dependsOn) || !node.dependsOn.every((id) => typeof id === 'string'))),
      )
    ) {
      return { definition: original, restored: false, notice: '浏览器草稿格式已失效，已展示服务加载版本。' };
    }
    return { definition: draft, restored: true };
  } catch {
    return { definition: original, restored: false, notice: '无法读取浏览器草稿，当前展示服务加载版本。' };
  }
}

export function saveDraft(definition: Definition): string | undefined {
  if (hasUnsafeNumber(definition)) return UNSAFE_NUMBER_MESSAGE;
  try {
    localStorage.setItem(draftStorageKey(definition), JSON.stringify({ format: 1, definition }));
    return undefined;
  } catch {
    return '浏览器无法保存草稿。请导出 YAML 保留已完成的修改。';
  }
}

// These are explicit editor initial values, not implicit schema defaults in the execution engine.
export function initialValue(schema: Schema): JsonValue {
  if (schema.enum?.length) return structuredClone(schema.enum[0]);
  switch (schema.type) {
    case 'string':
      return '';
    case 'boolean':
      return false;
    case 'number':
    case 'integer':
      return typeof schema.minimum === 'number' ? schema.minimum : 0;
    case 'array':
      return [];
    case 'object':
      return Object.fromEntries(
        Object.entries(schema.properties ?? {})
          .filter(([key]) => schema.required?.includes(key))
          .map(([key, child]) => [key, initialValue(child)]),
      );
    default:
      return null;
  }
}

export function createNode(descriptor: NodeDescriptor, id: string): WorkflowNode {
  return {
    id,
    type: descriptor.type,
    typeVersion: descriptor.typeVersion,
    title: descriptor.title,
    config: initialValue(descriptor.configSchema) as Record<string, JsonValue>,
    inputs: {},
    dependsOn: [],
    timeoutMs: 30000,
    retry: { maxAttempts: 1, backoffMs: 200 },
  };
}

export function nodeReferences(definition: Definition, nodeID: string): string[] {
  const references: string[] = [];
  for (const node of definition.nodes) {
    if (node.id === nodeID) continue;
    for (const [field, binding] of Object.entries(node.inputs ?? {})) {
      if (binding.kind === 'nodeOutput' && binding.nodeId === nodeID) references.push(`${node.id}.${field}`);
    }
    if (node.dependsOn?.includes(nodeID)) references.push(`${node.id} 的顺序依赖`);
  }
  for (const [field, binding] of Object.entries(definition.outputs ?? {})) {
    if (binding.kind === 'nodeOutput' && binding.nodeId === nodeID) references.push(`流程输出 ${field}`);
  }
  return references;
}

export function availableOutputs(definition: Definition, catalog: NodeDescriptor[], excludedID?: string) {
  return definition.nodes
    .filter((node) => node.id !== excludedID)
    .flatMap((node) => {
      const descriptor = catalog.find(
        (item) => typeKey(item.type, item.typeVersion) === typeKey(node.type, node.typeVersion),
      );
      return Object.entries(descriptor?.outputSchema.properties ?? {}).map(([field, schema]) => ({
        nodeId: node.id,
        field,
        schema,
      }));
    });
}

export function withBinding(bindings: Record<string, Binding> | undefined, field: string, binding?: Binding) {
  const result = { ...bindings };
  if (binding) result[field] = binding;
  else delete result[field];
  return result;
}
