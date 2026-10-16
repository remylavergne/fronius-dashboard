import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TooltipProvider } from '@/components/ui/tooltip'
import { App } from '@/App'
import './index.css'

const queryClient = new QueryClient()

const container = document.getElementById('root')
if (!container) {
  throw new Error('élément #root introuvable : le gabarit HTML a été modifié')
}

createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <App />
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>,
)
