import { createApp } from 'vue'
import 'uno.css'
import './styles.css'
import App from './App.vue'
import { store, setTheme } from './lib/state.js'

setTheme(store.theme)
createApp(App).mount('#app')