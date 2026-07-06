import './style.css'
import App from './App.svelte'
import { installFramelessResizeFix } from './lib/utils/framelessResizeFix'

installFramelessResizeFix()

const app = new App({
  target: document.getElementById('app')
})

export default app
