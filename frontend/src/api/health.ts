const apiBaseUrl = normalizeBaseUrl(import.meta.env.VITE_API_BASE_URL)

export interface HealthReport {
  status: 'ok'
}

export class ApiRequestError extends Error {
  readonly status?: number

  constructor(message: string, status?: number) {
    super(message)
    this.name = 'ApiRequestError'
    this.status = status
  }
}

export async function getHealth(signal?: AbortSignal): Promise<HealthReport> {
  const response = await fetch(`${apiBaseUrl}/healthz`, {
    headers: { Accept: 'application/json' },
    signal,
  })

  if (!response.ok) {
    throw new ApiRequestError(`Health check failed with status ${response.status}`, response.status)
  }

  const body: unknown = await response.json()
  if (!isHealthReport(body)) {
    throw new ApiRequestError('Health check returned an invalid response')
  }

  return body
}

function normalizeBaseUrl(value: string | undefined): string {
  return value?.replace(/\/+$/, '') ?? ''
}

function isHealthReport(value: unknown): value is HealthReport {
  return (
    typeof value === 'object' &&
    value !== null &&
    'status' in value &&
    value.status === 'ok'
  )
}
