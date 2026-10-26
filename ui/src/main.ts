import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { registerServiceWorker } from './lib/pwa'

mount(App, { target: document.getElementById('app')! })
registerServiceWorker()
