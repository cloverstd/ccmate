import { Link, useLocation, useNavigate, matchPath } from 'react-router-dom'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { projectsApi, tasksApi, subscribeToTasksEvents, type CurrentUser } from '../lib/api'
import Icon, { Logo } from './Icon'
import { Btn, Kbd } from './ui'

type Theme = 'light' | 'dark'
type Font = 'mono' | 'sans'
type Density = 'compact' | 'normal' | 'cozy'
type Prefs = { theme: Theme; font: Font; density: Density }

const DEFAULT_PREFS: Prefs = { theme: 'light', font: 'mono', density: 'normal' }
const PREFS_KEY = 'ccmate.prefs'

function loadPrefs(): Prefs {
  try {
    const saved = JSON.parse(localStorage.getItem(PREFS_KEY) || '{}')
    return { ...DEFAULT_PREFS, ...saved }
  } catch { return DEFAULT_PREFS }
}

export function applyPrefs(p: Prefs) {
  const r = document.documentElement
  r.dataset.theme = p.theme
  r.dataset.font = p.font
  r.dataset.density = p.density
}

export default function Layout({ children, user, onLogout }: { children: ReactNode; user: CurrentUser; onLogout: () => void }) {
  const location = useLocation()
  const navigate = useNavigate()
  const [prefs, setPrefs] = useState<Prefs>(loadPrefs)
  const [tweaksOpen, setTweaksOpen] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)

  useEffect(() => { applyPrefs(prefs); localStorage.setItem(PREFS_KEY, JSON.stringify(prefs)) }, [prefs])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault(); setPaletteOpen(true)
      } else if (e.key === 't' || e.key === 'T') {
        const tag = (e.target as HTMLElement | null)?.tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement | null)?.isContentEditable) return
        setPrefs((p) => ({ ...p, theme: p.theme === 'dark' ? 'light' : 'dark' }))
      } else if (e.key === 'Escape') {
        setPaletteOpen(false); setTweaksOpen(false)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const queryClient = useQueryClient()
  const { data: projects } = useQuery({ queryKey: ['projects'], queryFn: projectsApi.list })
  const { data: activeTasks } = useQuery({
    queryKey: ['tasks', 'active-count'],
    queryFn: () => tasksApi.list({ status: 'running' }),
  })
  const runningCount = activeTasks?.length ?? 0

  useEffect(() => {
    const unsub = subscribeToTasksEvents(() => {
      queryClient.invalidateQueries({ queryKey: ['tasks', 'active-count'] })
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
      queryClient.invalidateQueries({ queryKey: ['project-tasks'] })
    })
    return unsub
  }, [queryClient])

  const view: 'projects' | 'project' | 'tasks' | 'task' | 'settings' | 'other' = useMemo(() => {
    if (matchPath('/projects/:id', location.pathname)) return 'project'
    if (matchPath('/tasks/:id', location.pathname)) return 'task'
    if (location.pathname.startsWith('/projects')) return 'projects'
    if (location.pathname.startsWith('/tasks')) return 'tasks'
    if (location.pathname.startsWith('/settings')) return 'settings'
    return 'other'
  }, [location.pathname])

  const isTaskDetail = view === 'task'

  const projectMatch = matchPath('/projects/:id', location.pathname)
  const projectId = projectMatch ? Number((projectMatch.params as { id?: string }).id) : null

  const crumbs: Array<{ label: string; to?: string; cur?: boolean; dim?: boolean }> = []
  if (view === 'projects') crumbs.push({ label: 'projects', cur: true })
  else if (view === 'tasks') crumbs.push({ label: 'tasks', cur: true })
  else if (view === 'settings') crumbs.push({ label: 'settings', cur: true })
  else if (view === 'project') {
    const p = projects?.find((x) => x.id === projectId)
    crumbs.push({ label: 'projects', to: '/projects' })
    crumbs.push({ label: p?.name.split('/')[1] ?? `#${projectId}`, cur: true })
  } else if (view === 'task') {
    const taskMatch = matchPath('/tasks/:id', location.pathname)
    const taskId = taskMatch ? (taskMatch.params as { id?: string }).id : ''
    crumbs.push({ label: 'tasks', to: '/tasks' })
    crumbs.push({ label: `#${taskId}`, cur: true })
  }

  let mobileTitle = 'ccmate'
  if (view === 'tasks') mobileTitle = '$ tasks'
  else if (view === 'projects') mobileTitle = '$ projects'
  else if (view === 'settings') mobileTitle = '$ settings'
  else if (view === 'project') {
    const p = projects?.find((x) => x.id === projectId)
    mobileTitle = p ? `~/${p.name.split('/')[1]}` : 'project'
  } else if (view === 'task') {
    const taskMatch = matchPath('/tasks/:id', location.pathname)
    mobileTitle = `#${(taskMatch?.params as { id?: string } | undefined)?.id ?? ''}`
  }

  const toggleTheme = () => setPrefs((p) => ({ ...p, theme: p.theme === 'dark' ? 'light' : 'dark' }))

  return (
    <div className="app">
      {/* Sidebar (desktop) */}
      <aside className="sidebar">
        <Link to="/" className="sb-brand" style={{ textDecoration: 'none', color: 'inherit' }}>
          <Logo />
          <span className="word">ccmate<span className="brand-caret"/></span>
        </Link>
        <div className="sb-nav">
          <button className="sb-item" onClick={() => setPaletteOpen(true)} style={{ background: 'var(--bg-2)', marginBottom: 6 }}>
            <Icon name="search" className="ico"/>
            <span className="text-muted" style={{ fontFamily: 'var(--font-mono)', fontSize: 11 }}>Jump to...</span>
            <span style={{ marginLeft: 'auto' }}><Kbd>⌘K</Kbd></span>
          </button>
          <NavItem to="/tasks" icon="list" label="Tasks" count={runningCount} active={view === 'tasks' || view === 'task'} />
          <NavItem to="/projects" icon="folder" label="Projects" count={projects?.length} active={view === 'projects' || view === 'project'} />
          <NavItem to="/settings" icon="settings" label="Settings" active={view === 'settings'} />
        </div>
        <div className="sb-section">
          <span>projects</span>
        </div>
        <div className="sb-nav" style={{ overflowY: 'auto', flex: 1, minHeight: 0 }}>
          {(projects || []).map((p) => {
            const active = view === 'project' && projectId === p.id
            const shortName = p.name.split('/')[1] ?? p.name
            return (
              <Link key={p.id} to={`/projects/${p.id}`}
                className={`sb-project ${active ? 'active' : ''}`}
                style={{ textDecoration: 'none' }}>
                <span className="dot" style={{ background: p.auto_mode ? 'var(--accent)' : 'var(--fg-dim)' }}/>
                <span className="name">{shortName}</span>
              </Link>
            )
          })}
        </div>
        <div className="sb-foot">
          <div className="sb-user">
            <div className="sb-avatar">{user.user.charAt(0)}</div>
            <span className="name mono">@{user.user}</span>
          </div>
          <button className="btn btn-icon btn-ghost" title="Tweaks" onClick={() => setTweaksOpen((v) => !v)}>
            <Icon name="sliders" size={13}/>
          </button>
          <button className="btn btn-icon btn-ghost" title="Logout" onClick={onLogout}>
            <Icon name="logout" size={13}/>
          </button>
        </div>
      </aside>

      {/* Mobile top */}
      <div className="mobile-top">
        {(view === 'task' || view === 'project') ? (
          <button className="btn btn-icon btn-ghost" onClick={() => navigate(view === 'task' ? '/tasks' : '/projects')} aria-label="Back">
            <Icon name="back" size={16}/>
          </button>
        ) : (
          <Logo size={24}/>
        )}
        <span className="m-title">{mobileTitle}</span>
        <button className="btn btn-icon btn-ghost" onClick={() => setTweaksOpen((v) => !v)} aria-label="Tweaks">
          <Icon name="sliders" size={14}/>
        </button>
      </div>

      {/* Main */}
      <div className="main">
        <div className="topbar">
          <div className="breadcrumbs">
            <span style={{ color: 'var(--accent)' }}>❯</span>
            {crumbs.map((c, i) => (
              <span key={i} className="row-flex" style={{ gap: 6 }}>
                {i > 0 && <span className="sep">/</span>}
                {c.to ? <Link to={c.to} className="lnk">{c.label}</Link>
                  : <span className={c.cur ? 'cur trunc' : c.dim ? 'text-dim trunc' : 'trunc'}>{c.label}</span>}
              </span>
            ))}
          </div>
          <div className="topbar-actions">
            <Btn variant="ghost" size="sm" onClick={() => setPaletteOpen(true)}>
              <Icon name="search" size={13}/> Jump <Kbd>⌘K</Kbd>
            </Btn>
            <Btn variant="ghost" size="icon" title="Toggle theme" onClick={toggleTheme}>
              <Icon name={prefs.theme === 'dark' ? 'sun' : 'moon'} size={14}/>
            </Btn>
            <Btn variant="ghost" size="icon" title="Tweaks" onClick={() => setTweaksOpen((v) => !v)}>
              <Icon name="sliders" size={14}/>
            </Btn>
          </div>
        </div>

        <div className={`content ${isTaskDetail ? 'no-pad' : ''}`}
          style={isTaskDetail ? { display: 'flex', flexDirection: 'column', overflow: 'hidden', padding: 0 } : undefined}>
          {isTaskDetail ? children : <div className="page">{children}</div>}
        </div>
      </div>

      {/* Mobile tab bar */}
      <nav className="mtabbar">
        <MobileTab to="/tasks" icon="list" label="tasks" active={view === 'tasks' || view === 'task'} />
        <MobileTab to="/projects" icon="folder" label="repos" active={view === 'projects' || view === 'project'} />
        <button className="mtab" onClick={() => setPaletteOpen(true)}>
          <Icon name="search" size={18} className="ico"/>
          <span>jump</span>
        </button>
        <MobileTab to="/settings" icon="settings" label="settings" active={view === 'settings'} />
      </nav>

      {tweaksOpen && <TweakPanel prefs={prefs} setPrefs={setPrefs} onClose={() => setTweaksOpen(false)} />}
      {paletteOpen && <CommandPalette onClose={() => setPaletteOpen(false)} projects={projects || []} />}
    </div>
  )
}

function NavItem({ to, icon, label, count, active }: { to: string; icon: Parameters<typeof Icon>[0]['name']; label: string; count?: number; active: boolean }) {
  return (
    <Link to={to} className={`sb-item ${active ? 'active' : ''}`} style={{ textDecoration: 'none' }}>
      <Icon name={icon} className="ico"/> {label}
      {count != null && count > 0 && <span className="count">{count}</span>}
    </Link>
  )
}

function MobileTab({ to, icon, label, active }: { to: string; icon: Parameters<typeof Icon>[0]['name']; label: string; active: boolean }) {
  return (
    <Link to={to} className={`mtab ${active ? 'active' : ''}`} style={{ textDecoration: 'none' }}>
      <Icon name={icon} size={18} className="ico"/>
      <span>{label}</span>
    </Link>
  )
}

function TweakPanel({ prefs, setPrefs, onClose }: { prefs: Prefs; setPrefs: (p: Prefs) => void; onClose: () => void }) {
  return (
    <div className="tweak-panel">
      <div className="tweak-head">
        <Icon name="sliders" size={12}/>
        <span>Tweaks</span>
        <button className="btn btn-icon btn-ghost" style={{ marginLeft: 'auto', width: 20, height: 20 }} onClick={onClose} aria-label="Close">
          <Icon name="close" size={11}/>
        </button>
      </div>
      <div className="tweak-body">
        <Seg label="Theme" value={prefs.theme} options={['light', 'dark']} onChange={(v) => setPrefs({ ...prefs, theme: v as Theme })}/>
        <Seg label="Font" value={prefs.font} options={['mono', 'sans']} onChange={(v) => setPrefs({ ...prefs, font: v as Font })}/>
        <Seg label="Density" value={prefs.density} options={['compact', 'normal', 'cozy']} onChange={(v) => setPrefs({ ...prefs, density: v as Density })}/>
        <div style={{ fontSize: 10, color: 'var(--fg-dim)', marginTop: 4 }}>
          Press <Kbd>T</Kbd> to toggle theme · <Kbd>⌘K</Kbd> palette
        </div>
      </div>
    </div>
  )
}

function Seg({ label, value, options, onChange }: { label: string; value: string; options: string[]; onChange: (v: string) => void }) {
  return (
    <div className="tweak-row">
      <span>{label}</span>
      <div className="tweak-seg">
        {options.map((o) => (
          <button key={o} className={value === o ? 'on' : ''} onClick={() => onChange(o)}>{o}</button>
        ))}
      </div>
    </div>
  )
}

function CommandPalette({ onClose, projects }: { onClose: () => void; projects: { id: number; name: string }[] }) {
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const [idx, setIdx] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  const items = useMemo(() => {
    const base: Array<{ id: string; title: string; desc: string; icon: Parameters<typeof Icon>[0]['name']; go: () => void }> = [
      { id: 'n:tasks', title: 'Go to Tasks', desc: 'all tasks', icon: 'list', go: () => navigate('/tasks') },
      { id: 'n:projects', title: 'Go to Projects', desc: 'repos', icon: 'folder', go: () => navigate('/projects') },
      { id: 'n:settings', title: 'Go to Settings', desc: '', icon: 'settings', go: () => navigate('/settings') },
      ...projects.map((p) => ({
        id: `p:${p.id}`, title: p.name, desc: 'project', icon: 'folder' as const, go: () => navigate(`/projects/${p.id}`),
      })),
    ]
    const qq = q.toLowerCase()
    return qq ? base.filter((i) => i.title.toLowerCase().includes(qq) || i.desc.toLowerCase().includes(qq)) : base
  }, [q, projects, navigate])

  useEffect(() => { setIdx(0); inputRef.current?.focus() }, [q])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'ArrowDown') { e.preventDefault(); setIdx((i) => Math.min(i + 1, items.length - 1)) }
      else if (e.key === 'ArrowUp') { e.preventDefault(); setIdx((i) => Math.max(i - 1, 0)) }
      else if (e.key === 'Enter') {
        const it = items[idx]
        if (it) { it.go(); onClose() }
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [items, idx, onClose])

  return (
    <div className="palette-scrim" onClick={onClose}>
      <div className="palette" onClick={(e) => e.stopPropagation()}>
        <div className="palette-input">
          <Icon name="search" size={14} style={{ color: 'var(--fg-muted)' }}/>
          <input ref={inputRef} autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="jump to project or command..."/>
          <Kbd>ESC</Kbd>
        </div>
        <div className="palette-list">
          {items.slice(0, 16).map((it, i) => (
            <div key={it.id} className={`palette-item ${i === idx ? 'active' : ''}`}
              onMouseEnter={() => setIdx(i)} onClick={() => { it.go(); onClose() }}>
              <Icon name={it.icon} className="ico"/>
              <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{it.title}</span>
              <span className="desc">{it.desc}</span>
            </div>
          ))}
          {items.length === 0 && <div className="empty" style={{ padding: 24 }}>no matches</div>}
        </div>
      </div>
    </div>
  )
}
