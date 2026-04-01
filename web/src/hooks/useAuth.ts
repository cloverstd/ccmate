import { useState, useEffect } from 'react'
import { authApi, type CurrentUser } from '../lib/api'

export function useAuth() {
  const [user, setUser] = useState<CurrentUser | null>(null)
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    authApi.checkSession().then((u) => {
      setUser(u)
      setIsLoading(false)
    })
  }, [])

  return {
    user,
    isAuthenticated: !!user,
    isLoading,
    setUser,
    logout: async () => {
      await authApi.logout()
      setUser(null)
    },
  }
}
