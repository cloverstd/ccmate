import type { ReactNode } from 'react'

export function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <div className={`term ${className.replace(/!?mb-\d+/g, '')}`} style={{ marginBottom: className.includes('!mb-0') ? 0 : 20 }}>{children}</div>
}

export function CardHeader({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  return (
    <div className="term-head" style={{ height: 'auto', padding: '10px 14px', alignItems: 'flex-start', flexDirection: 'column', gap: 2 }}>
      <div className="row-flex" style={{ width: '100%' }}>
        <div className="dots"><span className="dot-tc"/><span className="dot-tc"/><span className="dot-tc"/></div>
        <span className="title" style={{ fontWeight: 600, color: 'var(--fg)' }}>$ {title}</span>
        {action && <div className="sp">{action}</div>}
      </div>
      {description && <div style={{ fontSize: 11, color: 'var(--fg-dim)', paddingLeft: 32 }}>{description}</div>}
    </div>
  )
}

export function CardContent({ children, className = '' }: { children: ReactNode; className?: string }) {
  const padOverride = className.includes('!p-0')
  const padY = className.includes('!py-')
  return (
    <div className={className.replace(/![^\s]+/g, '').trim()} style={{
      padding: padOverride ? 0 : padY ? '16px 14px' : 14,
      display: 'flex', flexDirection: 'column', gap: 14,
    }}>
      {children}
    </div>
  )
}

export function CardFooter({ children }: { children: ReactNode }) {
  return (
    <div style={{ padding: '10px 14px', borderTop: '1px solid var(--border)', background: 'var(--bg-1)', display: 'flex', alignItems: 'center', gap: 8 }}>
      {children}
    </div>
  )
}

export function Label({ children, htmlFor }: { children: ReactNode; htmlFor?: string }) {
  return <label htmlFor={htmlFor} className="label">{children}</label>
}

export function Input({ className = '', ...props }: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={`input ${className}`} />
}

export function Select({ className = '', children, ...props }: React.SelectHTMLAttributes<HTMLSelectElement> & { children: ReactNode }) {
  return <select {...props} className={`select ${className}`}>{children}</select>
}

export function Checkbox({ label, checked, onChange, description }: { label: string; checked: boolean; onChange: (v: boolean) => void; description?: string }) {
  return (
    <label className="row-flex" style={{ gap: 8, cursor: 'pointer', alignItems: 'flex-start' }}>
      <span style={{
        width: 14, height: 14, borderRadius: 3, marginTop: 2,
        border: '1.5px solid ' + (checked ? 'var(--accent)' : 'var(--border-strong)'),
        background: checked ? 'var(--accent)' : 'var(--surface)',
        display: 'grid', placeItems: 'center', flexShrink: 0,
        transition: 'all 0.12s',
      }}>
        {checked && (
          <svg viewBox="0 0 20 20" width={10} height={10} aria-hidden="true">
            <polyline points="4 10 8 14 16 6" fill="none" stroke="var(--accent-fg)" strokeWidth={2.5} strokeLinecap="round" strokeLinejoin="round"/>
          </svg>
        )}
      </span>
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} style={{ display: 'none' }} />
      <div style={{ minWidth: 0 }}>
        <span style={{ fontSize: 12.5 }}>{label}</span>
        {description && <div style={{ fontSize: 11, color: 'var(--fg-dim)', marginTop: 2 }}>{description}</div>}
      </div>
    </label>
  )
}

export function Btn({
  variant = 'primary',
  size = 'default',
  children,
  className = '',
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost' | 'accent'
  size?: 'default' | 'sm' | 'icon'
}) {
  const variantClass = {
    primary: 'btn-primary',
    secondary: '',
    danger: 'btn-danger',
    ghost: 'btn-ghost',
    accent: 'btn-accent',
  }[variant]
  const sizeClass = size === 'sm' ? 'btn-sm' : size === 'icon' ? 'btn-icon' : ''
  // Strip legacy tailwind color classes to avoid visual clashes.
  const cleaned = className
    .replace(/\bbg-[a-z-]+\d*\b/g, '')
    .replace(/\bhover:bg-[a-z-]+\d*\b/g, '')
    .replace(/\b!px-\d\b/g, '')
    .trim()
  return <button {...props} className={`btn ${variantClass} ${sizeClass} ${cleaned}`.trim()}>{children}</button>
}

export function Tag({ children, onRemove, color }: { children: ReactNode; onRemove?: () => void; color?: 'blue' | 'gray' | 'green' | 'yellow' | 'red' | 'purple' }) {
  const variantMap: Record<string, string> = {
    green: 'ok', red: 'err', yellow: 'warn', purple: 'wait', blue: '', gray: 'ghost',
  }
  const cls = color ? variantMap[color] ?? '' : ''
  return (
    <span className={`chip ${cls}`}>
      {children}
      {onRemove && <button onClick={onRemove} style={{ marginLeft: 2, color: 'var(--fg-dim)' }} aria-label="remove">×</button>}
    </span>
  )
}

export function Alert({ children, variant = 'info' }: { children: ReactNode; variant?: 'info' | 'warning' | 'error' }) {
  const cls = variant === 'warning' ? 'warn' : variant === 'error' ? 'err' : 'info'
  return <div className={`banner ${cls}`}>{children}</div>
}

export function Separator() {
  return <div className="divider-h" />
}

export function EmptyState({ children }: { children: ReactNode }) {
  return <div className="empty" style={{ padding: '32px 16px' }}>{children}</div>
}

export function Kbd({ children }: { children: ReactNode }) {
  return <span className="kbd">{children}</span>
}

export function TermCard({
  title,
  prompt = '$',
  actions,
  children,
  noPad,
}: { title?: string; prompt?: string; actions?: ReactNode; children: ReactNode; noPad?: boolean }) {
  return (
    <div className="term">
      <div className="term-head">
        <div className="dots"><span className="dot-tc"/><span className="dot-tc"/><span className="dot-tc"/></div>
        {title && <span className="title">{prompt} {title}</span>}
        {actions && <div className="sp">{actions}</div>}
      </div>
      <div className="term-body" style={noPad ? { padding: 0 } : undefined}>{children}</div>
    </div>
  )
}

export function SideCard({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <div className="side-card">
      <div className="side-card-head">
        <span>{title}</span>
        {action}
      </div>
      <div className="side-card-body">{children}</div>
    </div>
  )
}
