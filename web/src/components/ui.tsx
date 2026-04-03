import type { ReactNode } from 'react'

export function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <div className={`rounded-xl border border-gray-200 bg-white shadow-sm mb-6 ${className}`}>{children}</div>
}

export function CardHeader({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 px-6 py-4 border-b border-gray-100">
      <div>
        <h2 className="text-base font-semibold text-gray-900">{title}</h2>
        {description && <p className="text-sm text-gray-500 mt-0.5">{description}</p>}
      </div>
      {action}
    </div>
  )
}

export function CardContent({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <div className={`px-6 py-5 space-y-5 ${className}`}>{children}</div>
}

export function CardFooter({ children }: { children: ReactNode }) {
  return <div className="px-6 py-4 border-t border-gray-100 bg-gray-50/50 rounded-b-xl flex items-center gap-3">{children}</div>
}

export function Label({ children, htmlFor }: { children: ReactNode; htmlFor?: string }) {
  return <label htmlFor={htmlFor} className="block text-sm font-medium text-gray-700 mb-1.5">{children}</label>
}

export function Input({ className = '', ...props }: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={`w-full h-9 px-3 rounded-lg border border-gray-300 text-sm bg-white shadow-sm transition-colors placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500/20 focus:border-blue-500 disabled:bg-gray-100 disabled:text-gray-500 ${className}`} />
}

export function Select({ className = '', children, ...props }: React.SelectHTMLAttributes<HTMLSelectElement> & { children: ReactNode }) {
  return <select {...props} className={`h-9 px-3 rounded-lg border border-gray-300 text-sm bg-white shadow-sm transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500/20 focus:border-blue-500 ${className}`}>{children}</select>
}

export function Checkbox({ label, checked, onChange, description }: { label: string; checked: boolean; onChange: (v: boolean) => void; description?: string }) {
  return (
    <label className="flex items-start gap-3 cursor-pointer group">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)}
        className="mt-0.5 h-4 w-4 rounded border-gray-300 text-blue-600 shadow-sm focus:ring-blue-500/20" />
      <div>
        <span className="text-sm font-medium text-gray-700 group-hover:text-gray-900">{label}</span>
        {description && <p className="text-xs text-gray-500 mt-0.5">{description}</p>}
      </div>
    </label>
  )
}

export function Btn({ variant = 'primary', size = 'default', children, className = '', ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'secondary' | 'danger' | 'ghost'; size?: 'default' | 'sm' }) {
  const base = 'inline-flex items-center justify-center font-medium rounded-lg transition-colors focus:outline-none focus:ring-2 focus:ring-offset-1 disabled:opacity-50 disabled:pointer-events-none'
  const sizes = { default: 'h-9 px-4 text-sm', sm: 'h-7 px-3 text-xs' }
  const variants = {
    primary: 'bg-blue-600 text-white shadow-sm hover:bg-blue-700 focus:ring-blue-500',
    secondary: 'border border-gray-300 bg-white text-gray-700 shadow-sm hover:bg-gray-50 focus:ring-gray-400',
    danger: 'border border-red-200 bg-white text-red-600 shadow-sm hover:bg-red-50 focus:ring-red-400',
    ghost: 'text-gray-600 hover:bg-gray-100 hover:text-gray-900 focus:ring-gray-400',
  }
  return <button {...props} className={`${base} ${sizes[size]} ${variants[variant]} ${className}`}>{children}</button>
}

export function Tag({ children, onRemove, color = 'blue' }: { children: ReactNode; onRemove?: () => void; color?: 'blue' | 'gray' | 'green' | 'yellow' | 'red' | 'purple' }) {
  const colors: Record<string, string> = {
    blue: 'bg-blue-50 text-blue-700 border-blue-200',
    gray: 'bg-gray-50 text-gray-700 border-gray-200',
    green: 'bg-green-50 text-green-700 border-green-200',
    yellow: 'bg-yellow-50 text-yellow-700 border-yellow-200',
    red: 'bg-red-50 text-red-700 border-red-200',
    purple: 'bg-purple-50 text-purple-700 border-purple-200',
  }
  return (
    <span className={`inline-flex items-center gap-1 px-2.5 py-1 rounded-md border text-xs font-medium ${colors[color]}`}>
      {children}
      {onRemove && <button onClick={onRemove} className="ml-0.5 hover:text-red-500 transition-colors">&times;</button>}
    </span>
  )
}

export function Alert({ children, variant = 'info' }: { children: ReactNode; variant?: 'info' | 'warning' }) {
  const styles = { info: 'bg-blue-50 border-blue-200 text-blue-800', warning: 'bg-amber-50 border-amber-200 text-amber-800' }
  return <div className={`p-3 rounded-lg border text-xs ${styles[variant]}`}>{children}</div>
}

export function Separator() {
  return <div className="border-t border-gray-200" />
}

export function EmptyState({ children }: { children: ReactNode }) {
  return <p className="text-sm text-gray-400 text-center py-6">{children}</p>
}
