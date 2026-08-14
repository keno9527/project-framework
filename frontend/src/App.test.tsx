import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { getHealth } from '@/api/health'
import App from './App'

vi.mock('@/api/health', () => ({
  getHealth: vi.fn(),
}))

const getHealthMock = vi.mocked(getHealth)

describe('App', () => {
  beforeEach(() => {
    getHealthMock.mockReset()
  })

  it('shows a connected status when the API is healthy', async () => {
    getHealthMock.mockResolvedValue({ status: 'ok' })

    render(<App />)

    expect(screen.getByText('Checking API')).toBeInTheDocument()
    expect(await screen.findByText('Connected')).toBeInTheDocument()
  })

  it('shows an unavailable status when the health request fails', async () => {
    getHealthMock.mockRejectedValue(new Error('network unavailable'))

    render(<App />)

    expect(await screen.findByText('Unavailable')).toBeInTheDocument()
    expect(screen.getByText('network unavailable')).toBeInTheDocument()
  })
})
