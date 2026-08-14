import { useEffect, useState } from 'react'

import { getHealth } from '@/api/health'

type ConnectionState =
  | { phase: 'checking' }
  | { phase: 'connected' }
  | { phase: 'unavailable'; message: string }

const initialState: ConnectionState = { phase: 'checking' }

export function useHealthStatus() {
  const [connection, setConnection] = useState<ConnectionState>(initialState)

  useEffect(() => {
    const controller = new AbortController()

    getHealth(controller.signal)
      .then(() => {
        setConnection({ phase: 'connected' })
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) {
          return
        }
        setConnection({ phase: 'unavailable', message: errorMessage(error) })
      })

    return () => controller.abort()
  }, [])

  return { connection, status: statusCopy(connection) }
}

function statusCopy(connection: ConnectionState): { title: string; detail: string } {
  switch (connection.phase) {
    case 'checking':
      return { title: 'Checking API', detail: 'Waiting for the health endpoint.' }
    case 'connected':
      return { title: 'Connected', detail: 'The backend reports “ok”.' }
    case 'unavailable':
      return { title: 'Unavailable', detail: connection.message }
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'The API health check failed.'
}
