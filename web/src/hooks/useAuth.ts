import { useState, useEffect } from 'react'
import { authApi } from '../lib/api'

export function useAuth() {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    authApi.checkSession().then((authed) => {
      setIsAuthenticated(authed)
      setIsLoading(false)
    })
  }, [])

  return { isAuthenticated, isLoading, setIsAuthenticated }
}
