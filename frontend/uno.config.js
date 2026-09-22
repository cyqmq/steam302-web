import { defineConfig, presetWind } from 'unocss'

export default defineConfig({
  presets: [presetWind()],
  theme: {
    colors: {
      bg: 'var(--color-bg)',
      card: 'var(--color-card)',
      border: 'var(--color-border)',
      fg: 'var(--color-fg)',
      muted: 'var(--color-muted)',
      faint: 'var(--color-faint)',
      primary: 'var(--color-primary)',
      primaryhi: 'var(--color-primary-hi)'
    }
  }
})