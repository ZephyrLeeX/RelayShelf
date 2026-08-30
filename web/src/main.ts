import App from './App.vue'
import { bootstrapApplication } from './app/bootstrap'
import './app/styles/tokens.css'
import './app/styles/base.css'
import './app/styles/layout.css'
import { initializeTheme } from './app/composables/useTheme'

initializeTheme()
bootstrapApplication(App).mount('#app')
