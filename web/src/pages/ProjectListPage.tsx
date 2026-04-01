import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useState } from 'react'
import { projectsApi, type Project } from '../lib/api'

export default function ProjectListPage() {
  const queryClient = useQueryClient()
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({
    name: '',
    repo_url: '',
    git_provider: 'github',
    default_branch: 'main',
    auto_mode: false,
    max_concurrency: 2,
  })

  const { data: projects, isLoading } = useQuery({
    queryKey: ['projects'],
    queryFn: projectsApi.list,
  })

  const createMutation = useMutation({
    mutationFn: (data: Partial<Project>) => projectsApi.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      setShowForm(false)
      setForm({ name: '', repo_url: '', git_provider: 'github', default_branch: 'main', auto_mode: false, max_concurrency: 2 })
    },
  })

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">Projects</h1>
        <button
          onClick={() => setShowForm(!showForm)}
          className="px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-700"
        >
          {showForm ? 'Cancel' : 'New Project'}
        </button>
      </div>

      {showForm && (
        <div className="bg-white rounded-lg shadow p-6 mb-6">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Name</label>
              <input
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                className="w-full px-3 py-2 border border-gray-300 rounded text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Repository URL</label>
              <input
                value={form.repo_url}
                onChange={(e) => setForm({ ...form, repo_url: e.target.value })}
                placeholder="https://github.com/owner/repo"
                className="w-full px-3 py-2 border border-gray-300 rounded text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Default Branch</label>
              <input
                value={form.default_branch}
                onChange={(e) => setForm({ ...form, default_branch: e.target.value })}
                className="w-full px-3 py-2 border border-gray-300 rounded text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Max Concurrency</label>
              <input
                type="number"
                value={form.max_concurrency}
                onChange={(e) => setForm({ ...form, max_concurrency: parseInt(e.target.value) || 2 })}
                min={1}
                className="w-full px-3 py-2 border border-gray-300 rounded text-sm"
              />
            </div>
            <div className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={form.auto_mode}
                onChange={(e) => setForm({ ...form, auto_mode: e.target.checked })}
                id="auto_mode"
              />
              <label htmlFor="auto_mode" className="text-sm text-gray-700">Auto Mode</label>
            </div>
          </div>
          <button
            onClick={() => createMutation.mutate(form)}
            disabled={createMutation.isPending}
            className="mt-4 px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50"
          >
            {createMutation.isPending ? 'Creating...' : 'Create Project'}
          </button>
        </div>
      )}

      {isLoading ? (
        <div className="text-gray-500">Loading...</div>
      ) : (
        <div className="bg-white rounded-lg shadow overflow-hidden">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Repository</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Mode</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Concurrency</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {projects?.map((project) => (
                <tr key={project.id} className="hover:bg-gray-50">
                  <td className="px-6 py-4">
                    <Link to={`/projects/${project.id}`} className="text-blue-600 hover:underline font-medium">
                      {project.name}
                    </Link>
                  </td>
                  <td className="px-6 py-4 text-sm text-gray-600">{project.repo_url}</td>
                  <td className="px-6 py-4 text-sm">
                    <span className={`px-2 py-0.5 rounded text-xs ${project.auto_mode ? 'bg-green-100 text-green-700' : 'bg-gray-100 text-gray-600'}`}>
                      {project.auto_mode ? 'Auto' : 'Manual'}
                    </span>
                  </td>
                  <td className="px-6 py-4 text-sm text-gray-600">{project.max_concurrency}</td>
                </tr>
              ))}
              {(!projects || projects.length === 0) && (
                <tr>
                  <td colSpan={4} className="px-6 py-8 text-center text-gray-500">No projects yet</td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
