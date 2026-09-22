import { reactive } from 'vue'

export const store = reactive({
  rules: [],
  status: null,
  settings: null,
  version: null,
  accent: localStorage.getItem('accent') || 'red'
})

export function toast(msg, kind = 'ok') {
  const el = document.getElementById('toast')
  if (!el) return
  el.textContent = msg
  el.className = `show ${kind}`
  clearTimeout(el._t)
  el._t = setTimeout(() => {
    el.className = ''
  }, 2600)
}

export function setAccent(name) {
  store.accent = name
  localStorage.setItem('accent', name)
  document.documentElement.dataset.accent = name === 'red' ? '' : name
}