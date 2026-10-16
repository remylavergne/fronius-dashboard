import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'

// Vitest globals are disabled: RTL's automatic cleanup therefore does not
// install itself. Without it, renders accumulate between tests.
afterEach(cleanup)
