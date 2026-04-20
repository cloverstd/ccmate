import { Routes, Route, Navigate } from 'react-router-dom'
import { useEffect, useState } from 'react'
import Layout from './components/Layout'
import LoginPage from './pages/LoginPage'
import SetupPage from './pages/SetupPage'
import ProjectListPage from './pages/ProjectListPage'
import ProjectDetailPage from './pages/ProjectDetailPage'
import TaskListPage from './pages/TaskListPage'
import TaskDetailPage from './pages/TaskDetailPage'
import SettingsPage from './pages/SettingsPage'
import { useAuth } from './hooks/useAuth'

function App() {
  const { user, isAuthenticated, isLoading, logout } = useAuth()
  const [initialized, setInitialized] = useState<boolean | null>(null)

  useEffect(() => {
    fetch('/api/setup/status')
      .then((r) => r.json())
      .then((d) => setInitialized(d.initialized))
      .catch(() => setInitialized(true)) // assume initialized on error
  }, [])

  if (initialized === null || isLoading) {
    return (
      <div className="login-bg">
        <div className="mono text-dim">$ booting ccmate<span className="brand-caret" style={{ marginLeft: 4 }}/></div>
      </div>
    )
  }

  if (!initialized) {
    return <SetupPage />
  }

  if (!isAuthenticated) {
    return <LoginPage />
  }

  return (
    <Layout user={user!} onLogout={logout}>
      <Routes>
        <Route path="/" element={<Navigate to="/projects" replace />} />
        <Route path="/projects" element={<ProjectListPage />} />
        <Route path="/projects/:id" element={<ProjectDetailPage />} />
        <Route path="/tasks" element={<TaskListPage />} />
        <Route path="/tasks/:id" element={<TaskDetailPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Routes>
    </Layout>
  )
}

export default App
