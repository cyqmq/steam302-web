import { get } from './api.js'
import { store } from './state.js'

export async function loadRules() {
  const d = await get('/api/rules')
  store.rules = d?.rules || []
}
export async function loadStatus() {
  store.status = await get('/api/status')
}
export async function loadSettings() {
  store.settings = await get('/api/settings')
}
export async function loadVersion() {
  try {
    store.version = await get('/api/version')
  } catch {
    store.version = null
  }
}
export async function reloadAll() {
  await Promise.allSettled([loadRules(), loadStatus(), loadSettings()])
  await loadVersion()
}