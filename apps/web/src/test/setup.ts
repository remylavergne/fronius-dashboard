import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'

// Les globales vitest sont désactivées : le nettoyage automatique de RTL ne
// s'installe donc pas tout seul. Sans cela, les rendus s'accumulent entre tests.
afterEach(cleanup)
