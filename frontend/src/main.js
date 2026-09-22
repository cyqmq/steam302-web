import { createApp } from 'vue'
import 'uno.css'
import './styles.css'
import App from './App.vue'
import { store, setAccent } from './lib/state.js'

setAccent(store.accent)
createApp(App).mount('#app')