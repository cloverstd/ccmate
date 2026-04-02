import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useState } from 'react'
import { projectsApi, githubApi, type Project, type GitHubRepo } from '../lib/api'

export default function ProjectListPage() {
  const queryClient = useQueryClient()
  const [showForm, setShowForm] = useState(false)
  const [selectedRepo, setSelectedRepo] = useState<GitHubRepo | null>(null)
  const [repoSearch, setRepoSearch] = useState('')
  const [repoDropdownOpen, setRepoDropdownOpen] = useState(false)
  const [autoMode, setAutoMode] = useState(false)

  const { data: projects, isLoading } = useQuery({
    queryKey: ['projects'],
    queryFn: projectsApi.list,
  })

  const { data: repos, isLoading: reposLoading } = useQuery({
    queryKey: ['github-repos'],
    queryFn: githubApi.listRepos,
    enabled: showForm,
  })

  const createMutation = useMutation({
    mutationFn: (data: Partial<Project>) => projectsApi.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      setShowForm(false)
      setSelectedRepo(null)
    },
  })

  const handleCreate = () => {
    if (!selectedRepo) return
    createMutation.mutate({
      name: selectedRepo.full_name,
      repo_url: selectedRepo.html_url,
      git_provider: 'github',
      default_branch: selectedRepo.default_branch,
      auto_mode: autoMode,
    })
  }

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">Projects</h1>
        <button onClick={() => setShowForm(!showForm)}
          className="px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-700">
          {showForm ? 'Cancel' : 'New Project'}
        </button>
      </div>

      {showForm && (
        <div className="bg-white rounded-lg shadow p-6 mb-6">
          <div className="mb-4 relative">
            <label className="block text-sm font-medium text-gray-700 mb-1">Select GitHub Repository</label>
            {reposLoading ? (
              <div className="text-sm text-gray-500 py-2">Loading repositories...</div>
            ) : (
              <div>
                <input
                  value={selectedRepo ? selectedRepo.full_name : repoSearch}
                  onChange={(e) => {
                    setRepoSearch(e.target.value)
                    setSelectedRepo(null)
                    setRepoDropdownOpen(true)
                  }}
                  onFocus={() => setRepoDropdownOpen(true)}
                  onBlur={() => setTimeout(() => setRepoDropdownOpen(false), 200)}
                  placeholder="Search repositories..."
                  className="w-full px-3 py-2 border border-gray-300 rounded text-sm"
                />
                {repoDropdownOpen && !selectedRepo && (
                  <div className="absolute z-10 w-full mt-1 bg-white border border-gray-200 rounded shadow-lg max-h-60 overflow-y-auto">
                    {(repos || [])
                      .filter((repo) => !repoSearch || repo.full_name.toLowerCase().includes(repoSearch.toLowerCase()) || (repo.description || '').toLowerCase().includes(repoSearch.toLowerCase()))
                      .map((repo) => (
                        <button key={repo.full_name} type="button"
                          onClick={() => { setSelectedRepo(repo); setRepoSearch(''); setRepoDropdownOpen(false) }}
                          className="w-full text-left px-3 py-2 hover:bg-blue-50 text-sm border-b border-gray-50 last:border-0">
                          <div className="font-medium">{repo.full_name} {repo.private && <span className="text-xs text-gray-400 ml-1">(private)</span>}</div>
                          {repo.description && <div className="text-xs text-gray-500 truncate">{repo.description}</div>}
                        </button>
                      ))}
                    {(repos || []).filter((repo) => !repoSearch || repo.full_name.toLowerCase().includes(repoSearch.toLowerCase())).length === 0 && (
                      <div className="px-3 py-2 text-sm text-gray-400">No matching repositories</div>
                    )}
                  </div>
                )}
              </div>
            )}
          </div>

          {selectedRepo && (
            <div className="space-y-3">
              <div className="grid grid-cols-1 md:grid-cols-3 gap-3 p-3 bg-gray-50 rounded text-sm">
                <div><span className="text-gray-500">Name:</span> <span className="font-medium">{selectedRepo.full_name}</span></div>
                <div><span className="text-gray-500">Branch:</span> <span className="font-medium">{selectedRepo.default_branch}</span></div>
                <div><span className="text-gray-500">URL:</span> <a href={selectedRepo.html_url} target="_blank" rel="noopener noreferrer" className="text-blue-600 hover:underline">{selectedRepo.html_url}</a></div>
              </div>
              <div className="flex items-center gap-2">
                <input type="checkbox" checked={autoMode} onChange={(e) => setAutoMode(e.target.checked)} id="auto_mode_check" />
                <label htmlFor="auto_mode_check" className="text-sm text-gray-700">Auto Mode (automatically create tasks when label matched)</label>
              </div>
              <button onClick={handleCreate} disabled={createMutation.isPending}
                className="px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50">
                {createMutation.isPending ? 'Creating...' : 'Create Project'}
              </button>
            </div>
          )}
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
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Branch</th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Mode</th>
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
                  <td className="px-6 py-4 text-sm text-gray-600">{project.default_branch}</td>
                  <td className="px-6 py-4 text-sm">
                    <span className={`px-2 py-0.5 rounded text-xs ${project.auto_mode ? 'bg-green-100 text-green-700' : 'bg-gray-100 text-gray-600'}`}>
                      {project.auto_mode ? 'Auto' : 'Manual'}
                    </span>
                  </td>
                </tr>
              ))}
              {(!projects || projects.length === 0) && (
                <tr><td colSpan={4} className="px-6 py-8 text-center text-gray-500">No projects yet</td></tr>
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
