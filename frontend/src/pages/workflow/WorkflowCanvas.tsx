import { memo, useMemo } from 'react';
import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MiniMap,
  Panel,
  Position,
  ReactFlow,
  type NodeProps,
} from '@xyflow/react';
import { layoutWorkflow, type GraphNode } from './graph';
import type { WorkflowView } from '@/api/workflow.types';

const WorkflowCard = memo(function WorkflowCard({ data, selected }: NodeProps<GraphNode>) {
  return (
    <div className={`workflow-card ${selected ? 'is-selected' : ''}`}>
      <Handle type="target" position={Position.Left} isConnectable={false} />
      <button
        className="node-select"
        onClick={() => data.onSelect(data.instanceId)}
        aria-label={`查看节点 ${data.instanceId}`}
        aria-pressed={selected}
      >
        <span className="node-heading">
          <span className="node-symbol" aria-hidden="true">
            ◇
          </span>
          <span className="node-title">{data.title}</span>
          <span className="node-chevron" aria-hidden="true">
            ↗
          </span>
        </span>
        <span className="node-instance">{data.instanceId}</span>
        <span className="node-type">{data.typeName}</span>
      </button>
      <Handle type="source" position={Position.Right} isConnectable={false} />
    </div>
  );
});

const nodeTypes = { workflow: WorkflowCard };

export default function WorkflowCanvas({
  view,
  selectedId,
  onSelect,
  onClear,
}: {
  view: WorkflowView;
  selectedId: string | null;
  onSelect: (id: string) => void;
  onClear: () => void;
}) {
  const graph = useMemo(() => layoutWorkflow(view, selectedId, onSelect), [view, selectedId, onSelect]);
  return (
    <div className="workflow-canvas" role="region" aria-label="只读流程画布" data-testid="workflow-canvas">
      <ReactFlow<GraphNode>
        nodes={graph.nodes}
        edges={graph.edges}
        nodeTypes={nodeTypes}
        onNodeClick={(_, node) => onSelect(node.id)}
        onPaneClick={onClear}
        nodesDraggable={false}
        nodesConnectable={false}
        edgesReconnectable={false}
        deleteKeyCode={null}
        selectionOnDrag={false}
        fitView
        fitViewOptions={{ padding: 0.22, maxZoom: 1 }}
        minZoom={0.15}
        maxZoom={1.8}
        ariaLabelConfig={{
          'controls.zoomIn.ariaLabel': '放大画布',
          'controls.zoomOut.ariaLabel': '缩小画布',
          'controls.fitView.ariaLabel': '适应画布',
        }}
      >
        <Background variant={BackgroundVariant.Dots} color="#d9dfd9" gap={22} size={1} />
        <Controls showInteractive={false} />
        <MiniMap
          style={{ width: 130, height: 88 }}
          pannable
          zoomable
          nodeColor="#d3e6dc"
          maskColor="rgba(246, 248, 245, .75)"
          ariaLabel="流程缩略图"
        />
        <Panel position="top-left">
          <div className="canvas-caption">
            <span className="live-dot" />
            定义视图 <span>·</span> 静态 DAG
          </div>
        </Panel>
        <Panel position="bottom-center">
          <span className="canvas-hint">拖动画布平移 · 滚轮缩放 · 选择节点查看详情</span>
        </Panel>
      </ReactFlow>
    </div>
  );
}
