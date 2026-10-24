import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import { NotificationsProvider } from './Notifications'
import './styles.css'
import './DropdownSelect.css'

if (/Linux/i.test(navigator.platform)) {
  document.documentElement.classList.add('linux-webview')
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode><NotificationsProvider><App /></NotificationsProvider></React.StrictMode>,
)
