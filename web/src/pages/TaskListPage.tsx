import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useState } from 'react'
import { tasksApi, projectsApi, type TaskStatus } from '../lib/api'
import StatusBadge from '../components/StatusBadge'

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

  const { data: projects } = useQuery({
    queryKey: ['projects'],
    queryFn: projectsApi.list,
  })

  const createMutation = useMutation({
    mutationFn: () => tasksApi.create(newTask),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['tasks'] }); setShowCreate(false); setNewTask({ project_id: 0, issue_number: 0 }) },
  })

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">Tasks</h1>
        <div className="flex gap-2">
          <select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)} className="px-3 py-2 border border-gray-300 rounded text-sm">
            <option value="">All Status</option>
            {statusOptions.filter(Boolean).map((s) => <option key={s} value={s}>{s}</option>)}
          </select>
          <button onClick={() => setShowCreate(!showCreate)} className="px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-700">
            {showCreate ? 'Cancel' : 'New Task'}
          </button>
        </div>
      </div>

      {showCreate && (
        <div className="bg-white rounded-lg shadow p-6 mb-6">
          <div className="flex gap-4 items-end">
            <div className="flex-1">
              <label className="block text-sm font-medium text-gray-700 mb-1">Project</label>
              <select value={newTask.project_id} onChange={(e) => setNewTask({...newTask, project_id: parseInt(e.target.value)})} className="w-full px-3 py-2 border rounded text-sm">
                <option value={0}>Select project</option>
                {projects?.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </div>
            <div className="flex-1">
              <label className="block text-sm font-medium text-gray-700 mb-1">Issue Number</label>
              <input type="number" value={newTask.issue_number || ''} onChange={(e) => setNewTask({...newTask, issue_number: parseInt(e.target.value) || 0})} className="w-full px-3 py-2 border rounded text-sm" placeholder="#" />
            </div>
            <button onClick={() => createMutation.mutate()} disabled={!newTask.project_id || !newTask.issue_number} className="px-4 py-2 bg-blue-600 text-white rounded text-sm disabled:opacity-50">Create</button>
          </div>
        </div>
      )}

      {isLoading ? <div className="text-gray-500">Loading...</div> : (
        <div className="bg-white rounded-lg shadow overflow-hidden">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">ID</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Project</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Issue</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Type</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {tasks?.map((task) => (
                <tr key={task.id} className="hover:bg-gray-50">
                  <td className="px-6 py-4"><Link to={`/tasks/${task.id}`} className="text-blue-600 hover:underline">#{task.id}</Link></td>
                  <td className="px-6 py-4 text-sm text-gray-600">{task.edges.project?.name || '-'}</td>
                  <td className="px-6 py-4 text-sm">#{task.issue_number}</td>
                  <td className="px-6 py-4 text-sm text-gray-600">{task.type}</td>
                  <td className="px-6 py-4"><StatusBadge status={task.status} /></td>
                  <td className="px-6 py-4 text-sm text-gray-500">{new Date(task.created_at).toLocaleString()}</td>
                </tr>
              ))}
              {(!tasks || tasks.length === 0) && <tr><td colSpan={6} className="px-6 py-8 text-center text-gray-500">No tasks</td></tr>}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
