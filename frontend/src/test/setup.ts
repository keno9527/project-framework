import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// Vitest globals are disabled, so register cleanup explicitly to avoid DOM
// accumulation across tests in the same file.
afterEach(() => {
  cleanup()
})
