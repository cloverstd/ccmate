import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useState, useEffect } from 'react'
import { tasksApi, projectsApi, subscribeToTasksEvents } from '../lib/api'
import StatusBadge from '../components/StatusBadge'
import { Btn, Input, Select, Label, TermCard, Tag } from '../components/ui'
import Icon from '../components/Icon'

const statusTabs: Array<{ key: string; label: string }> = [
  { key: '', label: 'all' },
  { key: 'running', label: 'running' },
  { key: 'queued', label: 'queued' },
  { key: 'waiting_user', label: 'waiting' },
  { key: 'paused', label: 'paused' },
  { key: 'succeeded', label: 'succeeded' },
  { key: 'failed', label: 'failed' },
  { key: 'cancelled', label: 'cancelled' },
]

export default function TaskListPage() {
  const queryClient = useQueryClient()
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [search, setSearch] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [newTask, setNewTask] = useState({ project_id: 0, issue_number: 0 })

  const { data: tasks, isLoading } = useQuery({
    queryKey: ['tasks', statusFilter],
    queryFn: () => tasksApi.list(statusFilter ? { status: statusFilter } : undefined),
  })
  const { data: projects } = useQuery({ queryKey: ['projects'], queryFn: projectsApi.list })

  useEffect(() => {
    const unsub = subscribeToTasksEvents((event) => {
      if (event.type === 'task.status' || event.type === 'task.completed' || event.type === 'task.failed' || event.type === 'task.created') {
        queryClient.invalidateQueries({ queryKey: ['tasks'] })
      }
    })
    return unsub
  }, [queryClient])

  const createMutation = useMutation({
    mutationFn: () => tasksApi.create(newTask),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['tasks'] }); setShowCreate(false); setNewTask({ project_id: 0, issue_number: 0 }) },
  })

  const filtered = (tasks || []).filter((t) => {
    if (!search) return true
    const q = search.toLowerCase()
    return (
      `#${t.id}`.includes(q) ||
      String(t.issue_number).includes(q) ||
      (t.edges.project?.name ?? '').toLowerCase().includes(q)
    )
  })

  return (
    <div>
      <div className="page-head">
        <div>
          <h1 className="page-title"><span className="prompt">$</span> tasks</h1>
          <div className="page-sub">{tasks?.length ?? 0} total{projects ? ` across ${projects.length} projects` : ''}</div>
        </div>
        <div className="spacer"/>
        <div className="row-flex wrap" style={{ minWidth: 0 }}>
          <div style={{ position: 'relative', width: 220, maxWidth: '55vw' }}>
            <Icon name="search" size={13} style={{ position: 'absolute', left: 10, top: 8, color: 'var(--fg-dim)' }}/>
            <Input placeholder="grep tasks..." value={search} onChange={(e) => setSearch(e.target.value)} style={{ paddingLeft: 30 }}/>
          </div>
          <Btn variant="primary" onClick={() => setShowCreate(!showCreate)}>
            <Icon name="plus" size={12}/> {showCreate ? 'Cancel' : 'New'}
          </Btn>
        </div>
      </div>

      <div className="tabs" style={{ marginBottom: 16 }}>
        {statusTabs.map((t) => (
          <button key={t.key || 'all'} className={`tab ${statusFilter === t.key ? 'active' : ''}`} onClick={() => setStatusFilter(t.key)}>
            {t.label}
          </button>
        ))}
      </div>

      {showCreate && (
        <div style={{ marginBottom: 16 }}>
          <TermCard title="create task" actions={<Btn size="sm" variant="ghost" onClick={() => setShowCreate(false)}><Icon name="close" size={11}/></Btn>}>
            <div className="grid-2">
              <div className="fg">
                <Label>Project</Label>
                <Select value={newTask.project_id} onChange={(e) => setNewTask({ ...newTask, project_id: parseInt(e.target.value) })}>
                  <option value={0}>Select project</option>
                  {projects?.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                </Select>
              </div>
              <div className="fg">
                <Label>Issue #</Label>
                <Input type="number" value={newTask.issue_number || ''} onChange={(e) => setNewTask({ ...newTask, issue_number: parseInt(e.target.value) || 0 })} placeholder="#"/>
              </div>
            </div>
            <hr className="divider-h"/>
            <div>
              <Btn variant="primary" onClick={() => createMutation.mutate()} disabled={!newTask.project_id || !newTask.issue_number || createMutation.isPending}>
                {createMutation.isPending ? 'Creating...' : 'Create Task'}
              </Btn>
            </div>
          </TermCard>
        </div>
      )}

      {isLoading ? (
        <div className="empty">Loading...</div>
      ) : filtered.length === 0 ? (
        <div className="empty">
          <pre className="ascii">{`  ┌─ no tasks ─┐
  │   .        │
  │    \\_○_/   │
  └────────────┘`}</pre>
          <div>No tasks match this filter.</div>
        </div>
      ) : (
        <div className="row-list">
          {filtered.map((task) => (
            <Link key={task.id} to={`/tasks/${task.id}`} className="row">
              <span className="num">#{task.id}</span>
              <StatusBadge status={task.status} />
              <span className="title">issue #{task.issue_number} · {task.type}</span>
              <div className="meta">
                {task.edges.project?.name && <Tag color="gray">{task.edges.project.name}</Tag>}
                <span className="time">{relTime(task.created_at)}</span>
                <Icon name="chevron" size={12} style={{ color: 'var(--fg-dim)' }}/>
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}

function relTime(iso: string): string {
  const d = new Date(iso).getTime()
  const s = Math.floor((Date.now() - d) / 1000)
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}
