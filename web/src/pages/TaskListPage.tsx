import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useState } from 'react'
import { tasksApi, projectsApi, type TaskStatus } from '../lib/api'
import StatusBadge from '../components/StatusBadge'
import { Card, CardHeader, CardContent, CardFooter, Label, Input, Select, Btn, EmptyState } from '../components/ui'

const statusOptions: (TaskStatus | '')[] = ['', 'queued', 'running', 'paused', 'waiting_user', 'succeeded', 'failed', 'cancelled']

export default function TaskListPage() {
  const queryClient = useQueryClient()
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [showCreate, setShowCreate] = useState(false)
  const [newTask, setNewTask] = useState({ project_id: 0, issue_number: 0 })

  const { data: tasks, isLoading } = useQuery({
    queryKey: ['tasks', statusFilter],
    queryFn: () => tasksApi.list(statusFilter ? { status: statusFilter } : undefined),
  })

  const { data: projects } = useQuery({ queryKey: ['projects'], queryFn: projectsApi.list })

  const createMutation = useMutation({
    mutationFn: () => tasksApi.create(newTask),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['tasks'] }); setShowCreate(false); setNewTask({ project_id: 0, issue_number: 0 }) },
  })

  return (
    <div>
      <div className="flex flex-wrap justify-between items-center gap-3 mb-6">
        <h1 className="text-2xl font-bold">Tasks</h1>
        <div className="flex gap-2">
          <Select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
            <option value="">All Status</option>
            {statusOptions.filter(Boolean).map((s) => <option key={s} value={s}>{s}</option>)}
          </Select>
          <Btn onClick={() => setShowCreate(!showCreate)}>{showCreate ? 'Cancel' : 'New'}</Btn>
        </div>
      </div>

      {showCreate && (
        <Card>
          <CardHeader title="Create Task" description="Run a task for a specific issue" />
          <CardContent>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <Label>Project</Label>
                <Select value={newTask.project_id} onChange={(e) => setNewTask({ ...newTask, project_id: parseInt(e.target.value) })} className="w-full">
                  <option value={0}>Select project</option>
                  {projects?.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                </Select>
              </div>
              <div>
                <Label>Issue Number</Label>
                <Input type="number" value={newTask.issue_number || ''} onChange={(e) => setNewTask({ ...newTask, issue_number: parseInt(e.target.value) || 0 })} placeholder="#" />
              </div>
            </div>
          </CardContent>
          <CardFooter>
            <Btn onClick={() => createMutation.mutate()} disabled={!newTask.project_id || !newTask.issue_number || createMutation.isPending}>
              {createMutation.isPending ? 'Creating...' : 'Create Task'}
            </Btn>
          </CardFooter>
        </Card>
      )}

      {isLoading ? (
        <div className="py-8 text-center text-gray-400 text-sm">Loading...</div>
      ) : !tasks || tasks.length === 0 ? (
        <Card><CardContent><EmptyState>No tasks</EmptyState></CardContent></Card>
      ) : (
        <Card className="!mb-0">
          <CardContent className="!p-0">
            <div className="divide-y divide-gray-100">
              {tasks.map((task) => (
                <Link key={task.id} to={`/tasks/${task.id}`}
                  className="block px-4 py-3 hover:bg-gray-50 transition-colors">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-sm font-semibold text-blue-600">#{task.id}</span>
                    <span className="text-sm text-gray-700 truncate">{task.edges.project?.name || '-'}</span>
                    <span className="text-xs text-gray-500">Issue #{task.issue_number}</span>
                    <StatusBadge status={task.status} />
                    <span className="text-xs text-gray-400 ml-auto hidden sm:block">{new Date(task.created_at).toLocaleString()}</span>
                  </div>
                </Link>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
