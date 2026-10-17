// Browser contract probe used by web/data-layer.test.mjs; no application routes involved.
import * as React from 'react'
import { createRoot } from 'react-dom/client'
import { VaultProvider, useVault, type Vault } from './vault'

declare global { interface Window { testVault: Vault } }
function Probe() {
  const vault = useVault()
  React.useLayoutEffect(() => { window.testVault = vault })
  React.useEffect(() => { void vault.loadList({}) }, [vault.loadList])
  return null
}
export function mountProbe() {
  createRoot(document.getElementById('probe')!).render(<VaultProvider><Probe /></VaultProvider>)
}
