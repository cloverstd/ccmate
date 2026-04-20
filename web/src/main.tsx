import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import App from './App'
import { ToastProvider } from './components/Toast'
import './index.css'

// Apply persisted UI prefs (theme/font/density) ASAP to avoid flash.
try {
  const prefs = JSON.parse(localStorage.getItem('ccmate.prefs') || '{}')
  const r = document.documentElement
  r.dataset.theme = prefs.theme || 'light'
  r.dataset.font = prefs.font || 'mono'
  r.dataset.density = prefs.density || 'normal'
} catch {
  document.documentElement.dataset.theme = 'light'
  document.documentElement.dataset.font = 'mono'
  document.documentElement.dataset.density = 'normal'
}

// iOS Safari ignores viewport user-scalable=no since iOS 10
// Must use JS to prevent pinch-to-zoom
document.addEventListener('gesturestart', (e) => e.preventDefault())
document.addEventListener('gesturechange', (e) => e.preventDefault())

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <ToastProvider>
          <App />
        </ToastProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
