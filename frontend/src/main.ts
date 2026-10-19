import '@fontsource-variable/literata/opsz.css'
import '@fontsource-variable/literata/opsz-italic.css'
import '@fontsource-variable/golos-text/index.css'
import './styles/tokens.css'
import './styles/base.css'

import { createApp } from 'vue'

import App from './App.vue'
import { createAppRouter } from './router'

createApp(App).use(createAppRouter()).mount('#app')
