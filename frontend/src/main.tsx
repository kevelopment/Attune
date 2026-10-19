import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import { NotificationsProvider } from './Notifications'
import './styles.css'
import './DropdownSelect.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode><NotificationsProvider><App /></NotificationsProvider></React.StrictMode>,
)
