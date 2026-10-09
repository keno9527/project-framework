import dagre from '@dagrejs/dagre';
import { MarkerType, Position, type Edge, type Node } from '@xyflow/react';
import { typeKey, type Binding, type NodeDescriptor, type WorkflowNode, type WorkflowView } from '@/api/workflow.types';

export interface GraphNodeData extends Record<string, unknown> {
  title: string;
  instanceId: string;
  typeName: string;
  category: string;
  onSelect: (id: string) => void;
}

export type GraphNode = Node<GraphNodeData, 'workflow'>;
export const NODE_WIDTH = 240;
export const NODE_HEIGHT = 106;

function invalid(message: string): never {
  throw new Error(`流程数据异常：${message}`);
}

function validText(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0;
}

export function validateWorkflow(view: WorkflowView): void {
  if (
    !view ||
    view.apiVersion !== 'workflow/v1' ||
    !validText(view.workflowId) ||
    !validText(view.definitionVersion)
  ) {
    invalid('缺少流程标识或协议版本不受支持。');
  }
  if (!Array.isArray(view.nodes) || !Array.isArray(view.edges) || !Array.isArray(view.nodeTypes)) {
    invalid('缺少节点、连线或节点描述。');
  }
  if (view.nodes.length === 0 || view.nodes.length > 50 || view.edges.length > 200) {
    invalid('节点或连线数量不符合 V1 范围。');
  }
  if (view.inputSchema?.type !== 'object' || view.outputSchema?.type !== 'object')
    invalid('缺少流程输入输出契约。');
  const descriptors = new Map<string, NodeDescriptor>();
  for (const descriptor of view.nodeTypes) {
    if (
      !descriptor ||
      ![
        descriptor.type,
        descriptor.typeVersion,
        descriptor.title,
        descriptor.description,
        descriptor.category,
      ].every(validText) ||
      descriptor.inputSchema?.type !== 'object' ||
      descriptor.outputSchema?.type !== 'object' ||
      descriptor.configSchema?.type !== 'object'
    ) {
      invalid('节点描述缺失或不完整。');
    }
    const key = typeKey(descriptor.type, descriptor.typeVersion);
    if (descriptors.has(key)) invalid('节点类型描述重复。');
    descriptors.set(key, descriptor);
  }
  const nodes = new Map<string, WorkflowNode>();
  for (const node of view.nodes) {
    if (!node || !validText(node.id) || nodes.has(node.id)) invalid('节点 ID 缺失或重复。');
    if (!descriptors.has(typeKey(node.type, node.typeVersion))) invalid(`节点 ${node.id} 的版本描述不存在。`);
    if (
      !Number.isFinite(node.timeoutMs) ||
      node.timeoutMs <= 0 ||
      !node.retry ||
      !Number.isInteger(node.retry.maxAttempts) ||
      node.retry.maxAttempts < 1 ||
      !Number.isFinite(node.retry.backoffMs)
    ) {
      invalid(`节点 ${node.id} 缺少有效执行策略。`);
    }
    nodes.set(node.id, node);
  }
  const edgeIDs = new Set<string>();
  const pairs = new Set<string>();
  const incoming = new Map(view.nodes.map((node) => [node.id, 0]));
  const outgoing = new Map(view.nodes.map((node) => [node.id, [] as string[]]));
  for (const edge of view.edges) {
    if (
      !edge ||
      !validText(edge.id) ||
      edgeIDs.has(edge.id) ||
      !nodes.has(edge.source) ||
      !nodes.has(edge.target)
    ) {
      invalid('连线重复或引用不存在的节点。');
    }
    const pair = JSON.stringify([edge.source, edge.target]);
    if (edge.source === edge.target || pairs.has(pair)) invalid('存在自引用或重复依赖。');
    edgeIDs.add(edge.id);
    pairs.add(pair);
    incoming.set(edge.target, incoming.get(edge.target)! + 1);
    outgoing.get(edge.source)!.push(edge.target);
  }
  const ready = [...incoming].filter(([, count]) => count === 0).map(([id]) => id);
  let visited = 0;
  for (let index = 0; index < ready.length; index++) {
    visited++;
    for (const target of outgoing.get(ready[index])!) {
      const remaining = incoming.get(target)! - 1;
      incoming.set(target, remaining);
      if (remaining === 0) ready.push(target);
    }
  }
  if (visited !== nodes.size) invalid('依赖关系包含环。');
  const declaredPairs = new Set<string>();
  const checkBinding = (binding: Binding, label: string, target?: string) => {
    if (!binding || typeof binding !== 'object') invalid(`${label} 的绑定无效。`);
    switch (binding.kind) {
      case 'literal':
        if (!Object.hasOwn(binding, 'value')) invalid(`${label} 缺少常量值。`);
        break;
      case 'workflowInput':
        if (!Object.hasOwn(view.inputSchema.properties ?? {}, binding.field))
          invalid(`${label} 的流程输入字段不存在。`);
        break;
      case 'nodeOutput': {
        const source = nodes.get(binding.nodeId);
        if (!source) invalid(`${label} 的来源节点不存在。`);
        const descriptor = descriptors.get(typeKey(source.type, source.typeVersion))!;
        if (!Object.hasOwn(descriptor.outputSchema.properties ?? {}, binding.field))
          invalid(`${label} 的来源字段不存在。`);
        if (target) declaredPairs.add(JSON.stringify([binding.nodeId, target]));
        break;
      }
      default:
        invalid(`${label} 使用了未知绑定类型。`);
    }
  };
  for (const node of view.nodes) {
    for (const [field, binding] of Object.entries(node.inputs ?? {}))
      checkBinding(binding, `${node.id}.${field}`, node.id);
    for (const source of node.dependsOn ?? []) {
      if (!nodes.has(source)) invalid(`节点 ${node.id} 的显式依赖不存在。`);
      declaredPairs.add(JSON.stringify([source, node.id]));
    }
  }
  for (const [field, binding] of Object.entries(view.outputs ?? {}))
    checkBinding(binding, `流程输出 ${field}`);
  if (pairs.size !== declaredPairs.size || [...pairs].some((pair) => !declaredPairs.has(pair))) {
    invalid('展示连线与输入绑定或显式依赖不一致。');
  }
}

export function descriptorFor(view: WorkflowView, node: WorkflowNode): NodeDescriptor {
  return view.nodeTypes.find(
    (descriptor) => descriptor.type === node.type && descriptor.typeVersion === node.typeVersion,
  )!;
}

// The adapter keeps backend graph semantics separate from React Flow's layout model.
export function layoutWorkflow(
  view: WorkflowView,
  selectedId: string | null,
  onSelect: (id: string) => void,
): { nodes: GraphNode[]; edges: Edge[] } {
  validateWorkflow(view);
  const graph = new dagre.graphlib.Graph();
  graph.setDefaultEdgeLabel(() => ({}));
  graph.setGraph({ rankdir: 'LR', nodesep: 52, ranksep: 76, marginx: 40, marginy: 40 });
  for (const node of view.nodes) graph.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
  for (const edge of view.edges) graph.setEdge(edge.source, edge.target);
  dagre.layout(graph);
  return {
    nodes: view.nodes.map((node) => {
      const descriptor = descriptorFor(view, node);
      const position = graph.node(node.id);
      return {
        id: node.id,
        type: 'workflow',
        width: NODE_WIDTH,
        height: NODE_HEIGHT,
        position: { x: position.x - NODE_WIDTH / 2, y: position.y - NODE_HEIGHT / 2 },
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
        selected: selectedId === node.id,
        draggable: false,
        connectable: false,
        deletable: false,
        data: {
          title: node.title || descriptor.title,
          instanceId: node.id,
          typeName: `${node.type}@${node.typeVersion}`,
          category: descriptor.category,
          onSelect,
        },
      };
    }),
    edges: view.edges.map((edge) => ({
      id: edge.id,
      source: edge.source,
      target: edge.target,
      type: 'smoothstep',
      markerEnd: { type: MarkerType.ArrowClosed, color: '#77918b', width: 18, height: 18 },
      style: { stroke: '#77918b', strokeWidth: 1.6 },
      selectable: false,
      deletable: false,
      reconnectable: false,
      ariaLabel: `${edge.source} 到 ${edge.target}`,
    })),
  };
}
