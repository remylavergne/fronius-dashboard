import { describe, expect, it } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import { DevicePanel } from './DevicePanel'
import type { Detection, Device } from '@/lib/schema'

const device: Device = {
  model: 'Fronius Primo GEN24 3.6 Plus',
  firmware: '1.24.3',
  serial_number: 'SIMULATED-0001',
  source: '/status/version',
  read_at_utc: '2026-10-16T07:00:00.000Z',
}

const detection: Detection = {
  read_at_utc: '2026-10-16T07:00:00.000Z',
  meter: { state: 'present', source: '/components/PowerMeter/readable', reason: '' },
  battery: { state: 'absent', source: '/components/BatteryManagementSystem/readable', reason: 'aucun composant indexé' },
  internal_api: { state: 'present', source: '/status/version', reason: '' },
  device_events: { state: 'present', source: '/status/activeEvents', reason: '' },
  last_error: '',
}

function resourceRow(label: string): HTMLElement {
  const cell = screen.getByText(label)
  const row = cell.closest('div[data-state]')
  expect(row).not.toBeNull()
  return row as HTMLElement
}

describe('DevicePanel', () => {
  it('affiche l’identité quand une lecture a abouti', () => {
    render(<DevicePanel device={device} detection={detection} />)

    expect(screen.getByText('Fronius Primo GEN24 3.6 Plus')).toBeInTheDocument()
    expect(screen.getByText('1.24.3')).toBeInTheDocument()
    expect(screen.getByText('SIMULATED-0001')).toBeInTheDocument()
    // La provenance et l'âge de la lecture sont visibles.
    expect(screen.getByText('/status/version')).toBeInTheDocument()
    expect(within(screen.getByTestId('device-read-at')).getByText(/il y a|à l’instant/)).toBeInTheDocument()
  })

  it('traduit les trois états de détection', () => {
    render(<DevicePanel device={device} detection={detection} />)

    const meter = resourceRow('Compteur')
    expect(meter).toHaveAttribute('data-state', 'present')
    expect(within(meter).getByText('présent')).toBeInTheDocument()

    const battery = resourceRow('Batterie')
    expect(battery).toHaveAttribute('data-state', 'absent')
    expect(within(battery).getByText('absent')).toBeInTheDocument()
    // Un état non « présent » porte toujours son motif.
    expect(within(battery).getByText('aucun composant indexé')).toBeInTheDocument()
  })

  it('distingue « absent » de « non déterminé »', () => {
    const unknown: Detection = {
      ...detection,
      read_at_utc: null,
      meter: { state: 'unknown', source: '/components/PowerMeter/readable', reason: 'accès refusé (401)' },
      internal_api: { state: 'unknown', source: '/status/version', reason: 'accès refusé (401)' },
      last_error: '/status/version : accès refusé (401)',
    }
    render(<DevicePanel device={null} detection={unknown} />)

    const meter = resourceRow('Compteur')
    expect(meter).toHaveAttribute('data-state', 'unknown')
    expect(within(meter).getByText('non déterminé')).toBeInTheDocument()
    expect(within(meter).getByText('accès refusé (401)')).toBeInTheDocument()

    // La dernière erreur de détection est mise en évidence.
    const lastError = screen.getByTestId('detection-last-error')
    expect(lastError).toHaveClass('text-destructive')
    expect(lastError.textContent).toContain('accès refusé (401)')
  })

  it('n’affiche aucun chiffre quand l’identité est indéterminée', () => {
    // Invariant « pas de zéro inventé » : sans lecture, l'identité entière est
    // rendue « — » et le bloc ne contient aucun nombre de remplissage.
    render(<DevicePanel device={null} detection={null} />)

    const identity = screen.getByTestId('device-identity')
    expect(within(identity).getAllByText('—')).toHaveLength(3)
    expect(identity.textContent).not.toMatch(/\d/)

    // Sans détection, chaque ressource est « non déterminé ».
    for (const label of ['Compteur', 'Batterie', 'API interne', 'Événements']) {
      const row = resourceRow(label)
      expect(row).toHaveAttribute('data-state', 'unknown')
      expect(within(row).getByText('non déterminé')).toBeInTheDocument()
    }
  })

  it('rend un tiret pour un champ d’identité absent, jamais une chaîne vide', () => {
    // Les schémas interdisent '' : une identité lue peut seulement porter null.
    render(<DevicePanel device={{ ...device, model: null, firmware: null, serial_number: null }} detection={detection} />)

    const identity = screen.getByTestId('device-identity')
    expect(within(identity).getAllByText('—')).toHaveLength(3)
    expect(identity.textContent).not.toContain('inconnu')
  })
})
