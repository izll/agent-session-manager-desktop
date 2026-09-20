import './style.css'
import { mount } from 'svelte'
import App from './App.svelte'
import { installFramelessResizeFix } from './lib/utils/framelessResizeFix'
import { translateBackendErrors } from './lib/utils/backendErrorBridge'

installFramelessResizeFix()

// Before anything can call the backend: a failure reported as a translation
// key has to become a sentence on its way out, wherever it is shown.
translateBackendErrors()

const target = document.getElementById('app')
if (!target) throw new Error('Application mount point is missing')

const app = mount(App, { target })

export default app
