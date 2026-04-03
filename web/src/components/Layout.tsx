import { Link, useLocation } from 'react-router-dom'
import { useState, type ReactNode } from 'react'
import type { CurrentUser } from '../lib/api'

const navItems = [
  { path: '/projects', label: 'Projects' },
  { path: '/tasks', label: 'Tasks' },
  { path: '/settings', label: 'Settings' },
]

export default function Layout({ children, user, onLogout }: { children: ReactNode; user: CurrentUser; onLogout: () => void }) {
  const location = useLocation()
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)

  return (
    <div className="h-screen bg-gray-50 flex flex-col overflow-hidden">
      <nav className="bg-white border-b border-gray-200 sticky top-0 z-50">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex justify-between h-14">
            <div className="flex items-center gap-6">
              <Link to="/" className="text-xl font-bold text-gray-900 shrink-0">ccmate</Link>
              <div className="hidden sm:flex gap-1">
                {navItems.map((item) => (
                  <Link key={item.path} to={item.path}
                    className={`px-3 py-2 text-sm rounded-md transition-colors ${
                      location.pathname.startsWith(item.path) ? 'bg-gray-100 text-gray-900 font-medium' : 'text-gray-600 hover:text-gray-900 hover:bg-gray-50'
                    }`}>
                    {item.label}
                  </Link>
                ))}
              </div>
            </div>
            <div className="flex items-center gap-3">
              <span className="text-sm text-gray-600 hidden sm:inline">{user.user}</span>
              <button onClick={onLogout} className="text-sm text-gray-500 hover:text-gray-700 hidden sm:inline">Logout</button>
              <button onClick={() => setMobileMenuOpen(!mobileMenuOpen)} className="sm:hidden p-2 rounded-md text-gray-600 hover:bg-gray-100">
                <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d={mobileMenuOpen ? 'M6 18L18 6M6 6l12 12' : 'M4 6h16M4 12h16M4 18h16'} /></svg>
              </button>
            </div>
          </div>
        </div>
        {mobileMenuOpen && (
          <div className="sm:hidden border-t border-gray-200 bg-white">
            <div className="px-4 py-2 space-y-1">
              {navItems.map((item) => (
                <Link key={item.path} to={item.path} onClick={() => setMobileMenuOpen(false)}
                  className={`block px-3 py-2.5 text-sm rounded-md ${
                    location.pathname.startsWith(item.path) ? 'bg-gray-100 text-gray-900 font-medium' : 'text-gray-600'
                  }`}>
                  {item.label}
                </Link>
              ))}
              <div className="border-t border-gray-100 pt-2 mt-2 flex items-center justify-between px-3 py-2">
                <span className="text-sm text-gray-600">{user.user}</span>
                <button onClick={onLogout} className="text-sm text-red-500">Logout</button>
              </div>
            </div>
          </div>
        )}
      </nav>
      <main className="flex-1 min-h-0 max-w-7xl w-full mx-auto px-4 sm:px-6 lg:px-8 py-4 sm:py-6 flex flex-col overflow-y-auto overflow-x-hidden">{children}</main>
    </div>
  )
}
