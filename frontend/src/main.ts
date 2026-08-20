import './style.css'
import { mount } from 'svelte'
import App from './App.svelte'
import { installFramelessResizeFix } from './lib/utils/framelessResizeFix'

installFramelessResizeFix()

const target = document.getElementById('app')
if (!target) throw new Error('Application mount point is missing')

const app = mount(App, { target })

export default app
