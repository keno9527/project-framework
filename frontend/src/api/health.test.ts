import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiRequestError, getHealth } from './health'

describe('getHealth', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns the typed health response', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ status: 'ok' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(getHealth()).resolves.toEqual({ status: 'ok' })
    expect(fetchMock).toHaveBeenCalledWith('/healthz', {
      headers: { Accept: 'application/json' },
      signal: undefined,
    })
  })

  it('throws a request error for non-success responses', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 503 })))

    await expect(getHealth()).rejects.toEqual(
      new ApiRequestError('Health check failed with status 503', 503),
    )
  })

  it.each([{ state: 'ok' }, { status: 'failed' }])(
    'rejects invalid success response $state$status',
    async (body) => {
      vi.stubGlobal(
        'fetch',
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify(body), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        ),
      )

      await expect(getHealth()).rejects.toThrow('Health check returned an invalid response')
    },
  )
})
