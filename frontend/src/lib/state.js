import { reactive } from 'vue'

export const store = reactive({
  rules: [],
  status: null,
  settings: null,
  version: null,
  theme: localStorage.getItem('theme') || 'auto'
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

export function setTheme(name) {
  store.theme = name
  localStorage.setItem('theme', name)
  document.documentElement.dataset.theme = name
}