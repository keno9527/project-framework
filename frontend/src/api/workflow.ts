import { useEffect, useState } from 'react';
import type { LoadState } from './workflow.types';

const apiBaseUrl = normalizeBaseUrl(import.meta.env.VITE_API_BASE_URL);

export class APIError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly status: number,
  ) {
    super(message);
    this.name = 'APIError';
  }
}

export async function fetchData<T>(path: string, signal?: AbortSignal): Promise<T> {
  return requestData<T>(path, { signal, headers: { Accept: 'application/json' } });
}

export async function postData<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> {
  return postJSONData<T>(path, JSON.stringify(body), signal);
}

// Preserve authored JSON numbers when input is edited as source text.
export async function postJSONData<T>(path: string, body: string, signal?: AbortSignal): Promise<T> {
  return requestData<T>(path, {
    method: 'POST',
    signal,
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body,
  });
}

async function requestData<T>(path: string, options: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(withBaseUrl(path), options);
  } catch (error) {
    if (options.signal?.aborted) throw error;
    throw new APIError('NETWORK_ERROR', '无法连接服务，请检查后端是否启动后重试。', 0);
  }
  let body: { data?: T; error?: { code?: string; message?: string } };
  try {
    body = await response.json();
  } catch {
    throw new APIError('INVALID_RESPONSE', '服务返回的数据格式无效，请检查 API 代理配置。', response.status);
  }
  if (!response.ok) {
    throw new APIError(
      body?.error?.code ?? 'HTTP_ERROR',
      body?.error?.message ?? `请求失败（${response.status}）`,
      response.status,
    );
  }
  if (!body || !Object.hasOwn(body, 'data')) {
    throw new APIError('INVALID_RESPONSE', '服务响应缺少 data 字段。', response.status);
  }
  return body.data as T;
}

export function errorMessage(error: unknown): string {
  if (error instanceof APIError) return `${error.message} · ${error.code}`;
  return error instanceof Error ? error.message : '发生未知错误，请重试。';
}

// The key and AbortController prevent an older request from replacing a newly selected version.
export function useAPI<T>(path: string, validate?: (data: T) => void) {
  const [reload, setReload] = useState(0);
  const [result, setResult] = useState<{ key: string; state: LoadState<T> }>({
    key: '',
    state: { status: 'loading' },
  });
  const key = `${path}:${reload}`;
  useEffect(() => {
    const controller = new AbortController();
    fetchData<T>(path, controller.signal)
      .then((data) => {
        validate?.(data);
        if (!controller.signal.aborted) setResult({ key, state: { status: 'ready', data } });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setResult({ key, state: { status: 'error', error: errorMessage(error) } });
      });
    return () => controller.abort();
  }, [key, path, validate]);
  return {
    state: result.key === key ? result.state : ({ status: 'loading' } as LoadState<T>),
    retry: () => setReload((count) => count + 1),
  };
}

function withBaseUrl(path: string): string {
  return `${apiBaseUrl}${path}`;
}

function normalizeBaseUrl(value: string | undefined): string {
  return value?.replace(/\/+$/, '') ?? '';
}
