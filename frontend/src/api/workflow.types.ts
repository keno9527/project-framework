export type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };

export interface Schema {
  type: string;
  title?: string;
  description?: string;
  properties?: Record<string, Schema>;
  required?: string[];
  items?: Schema;
  enum?: JsonValue[];
  default?: JsonValue;
  additionalProperties?: boolean;
  [key: string]: unknown;
}

export interface NodeDescriptor {
  type: string;
  typeVersion: string;
  title: string;
  description: string;
  category: string;
  configSchema: Schema;
  inputSchema: Schema;
  outputSchema: Schema;
  retrySafe: boolean;
  uiHints?: { iconKey?: string; fieldOrder?: string[]; advanced?: string[] };
}

export type Binding =
  | { kind: 'literal'; value: JsonValue }
  | { kind: 'workflowInput'; field: string }
  | { kind: 'nodeOutput'; nodeId: string; field: string };

export interface WorkflowNode {
  id: string;
  type: string;
  typeVersion: string;
  title?: string;
  config?: Record<string, JsonValue>;
  inputs?: Record<string, Binding>;
  dependsOn?: string[];
  timeoutMs: number;
  retry: { maxAttempts: number; backoffMs: number };
}

export interface WorkflowEdge {
  id: string;
  source: string;
  target: string;
  reasons?: string[];
  mappings?: { sourceField: string; targetField: string }[];
}

export interface WorkflowSummary {
  workflowId: string;
  definitionVersion: string;
  title: string;
  nodeCount: number;
}

export interface Definition extends Omit<WorkflowSummary, 'nodeCount'> {
  apiVersion: 'workflow/v1';
  inputSchema: Schema;
  outputSchema: Schema;
  outputs?: Record<string, Binding>;
  nodes: WorkflowNode[];
}

export interface WorkflowView extends Definition {
  edges: WorkflowEdge[];
  nodeTypes: NodeDescriptor[];
}

export interface ValidationResult {
  view: WorkflowView;
  yaml: string;
}

export type RunStatus =
  | 'PENDING'
  | 'RUNNING'
  | 'RETRY_WAIT'
  | 'SUCCEEDED'
  | 'FAILED'
  | 'TIMED_OUT'
  | 'CANCELLED'
  | 'SKIPPED';

export interface RunError {
  code: string;
  message: string;
  nodeId?: string;
  attempt?: number;
}

export interface AttemptSnapshot {
  attempt: number;
  status: RunStatus;
  startedAt: string;
  finishedAt?: string;
  error?: RunError;
}

export interface NodeSnapshot {
  nodeId: string;
  nodeExecutionId: string;
  status: RunStatus;
  attempt: number;
  attempts: AttemptSnapshot[];
  startedAt?: string;
  finishedAt?: string;
  error?: RunError;
}

export interface RunSnapshot {
  runId: string;
  workflowId: string;
  definitionVersion: string;
  status: RunStatus;
  instanceId: string;
  ephemeral: boolean;
  output?: Record<string, JsonValue>;
  nodes: NodeSnapshot[];
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  error?: RunError;
}

export type LoadState<T> =
  | { status: 'loading' }
  | { status: 'error'; error: string }
  | { status: 'ready'; data: T };

export const typeKey = (type: string, version: string) => JSON.stringify([type, version]);
export const workflowKey = (workflow: Pick<WorkflowSummary, 'workflowId' | 'definitionVersion'>) =>
  JSON.stringify([workflow.workflowId, workflow.definitionVersion]);
