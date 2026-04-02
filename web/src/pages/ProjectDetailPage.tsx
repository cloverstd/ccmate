import { useParams, Link, useLocation, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import Markdown from 'react-markdown'
import { projectsApi, tasksApi, promptsApi, type Task, type CommitInfo } from '../lib/api'
import StatusBadge from '../components/StatusBadge'

type Tab = 'info' | 'git' | 'issues' | 'tasks' | 'templates'

function Loading() {
  return <div className="py-8 text-center text-gray-400 text-sm">Loading...</div>
}

export default function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>()
  const projectId = parseInt(id || '0')
  const queryClient = useQueryClient()
  const location = useLocation()
  const navigate = useNavigate()
  const tab = (location.hash.replace('#', '') as Tab) || 'info'
  const setTab = (t: Tab) => navigate({ hash: t }, { replace: true })

  const [showPromptForm, setShowPromptForm] = useState(false)
  const [promptTitle, setPromptTitle] = useState('')
  const [promptBody, setPromptBody] = useState('')
  const [promptPreview, setPromptPreview] = useState(false)

  const { data: project, isLoading } = useQuery({
    queryKey: ['project', projectId], queryFn: () => projectsApi.get(projectId), enabled: projectId > 0,
  })
  const [selectedBranch, setSelectedBranch] = useState<string>('')
  const { data: gitInfo, isLoading: gitLoading, refetch: refetchGit } = useQuery({
    queryKey: ['project-git', projectId], queryFn: () => projectsApi.gitInfo(projectId), enabled: projectId > 0 && tab === 'git',
  })
  const { data: commits, isLoading: commitsLoading } = useQuery({
    queryKey: ['project-commits', projectId, selectedBranch],
    queryFn: () => projectsApi.commits(projectId, selectedBranch || 'HEAD'),
    enabled: projectId > 0 && tab === 'git' && !!gitInfo,
  })
  const pullMutation = useMutation({
    mutationFn: () => projectsApi.pull(projectId),
    onSuccess: () => refetchGit(),
  })

  const { data: tasks, isLoading: tasksLoading } = useQuery({
    queryKey: ['project-tasks', projectId], queryFn: () => projectsApi.listTasks(projectId), enabled: projectId > 0 && tab === 'tasks',
  })
  const { data: issues, isLoading: issuesLoading, refetch: refetchIssues } = useQuery({
    queryKey: ['project-issues', projectId], queryFn: () => projectsApi.listIssues(projectId), enabled: projectId > 0 && tab === 'issues',
  })
  const { data: prs, isLoading: prsLoading, refetch: refetchPRs } = useQuery({
    queryKey: ['project-prs', projectId], queryFn: () => projectsApi.listPRs(projectId), enabled: projectId > 0 && tab === 'issues',
  })
  const { data: templates, isLoading: templatesLoading } = useQuery({
    queryKey: ['prompt-templates'], queryFn: promptsApi.list, enabled: tab === 'templates',
  })
  const [newTemplate, setNewTemplate] = useState({ name: '', system_prompt: '', task_prompt: '' })
  const [showTemplateForm, setShowTemplateForm] = useState(false)

  const createTemplate = useMutation({
    mutationFn: () => promptsApi.create(newTemplate),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['prompt-templates'] }); setShowTemplateForm(false); setNewTemplate({ name: '', system_prompt: '', task_prompt: '' }) },
  })
  const deleteTemplate = useMutation({
    mutationFn: (tid: number) => promptsApi.delete(tid),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['prompt-templates'] }),
  })

  const createFromPrompt = useMutation({
    mutationFn: () => tasksApi.createFromPrompt({ project_id: projectId, title: promptTitle, body: promptBody }),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['project-tasks'] })
      queryClient.invalidateQueries({ queryKey: ['project-issues'] })
      setShowPromptForm(false); setPromptTitle(''); setPromptBody('')
      if (result.task?.id) navigate(`/tasks/${result.task.id}`)
    },
  })

  const createTaskForIssue = useMutation({
    mutationFn: (issueNumber: number) => tasksApi.create({ project_id: projectId, issue_number: issueNumber }),
    onSuccess: (task) => {
      queryClient.invalidateQueries({ queryKey: ['project-tasks'] })
      navigate(`/tasks/${task.id}`)
    },
  })

  if (isLoading) return <Loading />
  if (!project) return <div className="text-gray-500">Project not found</div>

  const tabs: { key: Tab; label: string }[] = [
    { key: 'info', label: 'Info' },
    { key: 'git', label: 'Git' },
    { key: 'issues', label: 'Issues & PRs' },
    { key: 'tasks', label: 'Tasks' },
    { key: 'templates', label: 'Prompt Templates' },
  ]

  return (
    <div>
      {/* Header with GitHub link */}
      <div className="flex items-center gap-3 mb-4">
        <h1 className="text-2xl font-bold">{project.name}</h1>
        <a href={project.repo_url} target="_blank" rel="noopener noreferrer"
          className="text-gray-400 hover:text-gray-600" title="Open on GitHub">
          <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/></svg>
        </a>
      </div>

      {/* Tab bar */}
      <div className="border-b border-gray-200 mb-6">
        <div className="flex gap-4">
          {tabs.map((t) => (
            <button key={t.key} onClick={() => setTab(t.key)}
              className={`pb-2 text-sm font-medium ${tab === t.key ? 'border-b-2 border-blue-600 text-blue-600' : 'text-gray-500 hover:text-gray-700'}`}>
              {t.label}
            </button>
          ))}
        </div>
      </div>

      {/* ====== Info tab ====== */}
      {tab === 'info' && (
        <div className="bg-white rounded-lg shadow p-6">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-500">Repository</label>
              <p className="mt-1 text-sm">
                <a href={project.repo_url} target="_blank" rel="noopener noreferrer" className="text-blue-600 hover:underline">{project.repo_url}</a>
              </p>
            </div>
            <div><label className="block text-sm font-medium text-gray-500">Default Branch</label><p className="mt-1 text-sm">{project.default_branch}</p></div>
            <div><label className="block text-sm font-medium text-gray-500">Mode</label><p className="mt-1 text-sm">{project.auto_mode ? 'Automatic' : 'Manual'}</p></div>
          </div>
        </div>
      )}

      {/* ====== Git tab ====== */}
      {tab === 'git' && (
        <div className="space-y-6">
          {/* Header with path and actions */}
          <div className="bg-white rounded-lg shadow p-6">
            <div className="flex justify-between items-center mb-4">
              <div>
                <h2 className="text-lg font-semibold">Repository</h2>
                {gitInfo?.repo_path && <p className="text-xs text-gray-400 font-mono mt-1">{gitInfo.repo_path}</p>}
              </div>
              <div className="flex gap-2">
                <button onClick={() => pullMutation.mutate()} disabled={pullMutation.isPending}
                  className="px-3 py-1 text-xs border rounded hover:bg-gray-50 disabled:opacity-50">
                  {pullMutation.isPending ? 'Fetching...' : 'Fetch Remote'}
                </button>
                <button onClick={() => refetchGit()} className="px-3 py-1 text-xs border rounded hover:bg-gray-50">Refresh</button>
              </div>
            </div>
          </div>

          {gitLoading ? <Loading /> : (
            <>
              {/* Branches */}
              <div className="bg-white rounded-lg shadow p-6">
                <h2 className="text-lg font-semibold mb-4">Branches ({gitInfo?.branches?.length || 0})</h2>
                <div className="space-y-1">
                  {(gitInfo?.branches || []).map((b) => (
                    <button key={b.name} onClick={() => setSelectedBranch(b.name)}
                      className={`w-full text-left flex items-center gap-3 py-2 px-3 rounded text-sm hover:bg-gray-50 ${selectedBranch === b.name ? 'bg-blue-50 border border-blue-200' : 'bg-gray-50'}`}>
                      <span className="font-medium shrink-0">{b.name}</span>
                      <code className="text-xs text-gray-400 shrink-0">{b.hash}</code>
                      <span className="text-xs text-gray-500 truncate">{b.message}</span>
                    </button>
                  ))}
                  {(!gitInfo?.branches || gitInfo.branches.length === 0) && <p className="text-sm text-gray-400">No branches found. Is the repo cloned?</p>}
                </div>
              </div>

              {/* Commits for selected branch */}
              {selectedBranch && (
                <div className="bg-white rounded-lg shadow p-6">
                  <h2 className="text-lg font-semibold mb-4">Commits on <code className="text-blue-600">{selectedBranch}</code></h2>
                  {commitsLoading ? <Loading /> : (
                    <div className="space-y-1">
                      {(commits as CommitInfo[] || []).map((c, i) => (
                        <div key={i} className="flex items-start gap-3 py-2 px-3 bg-gray-50 rounded text-sm">
                          <code className="text-xs text-blue-600 shrink-0 mt-0.5">{c.hash}</code>
                          <div className="flex-1 min-w-0">
                            <div className="truncate">{c.message}</div>
                            <div className="text-xs text-gray-400">{c.author} &middot; {c.date}</div>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {/* Tags */}
              <div className="bg-white rounded-lg shadow p-6">
                <h2 className="text-lg font-semibold mb-4">Tags ({gitInfo?.tags?.length || 0})</h2>
                {(gitInfo?.tags || []).length > 0 ? (
                  <div className="flex flex-wrap gap-2">
                    {(gitInfo?.tags || []).map((t) => (
                      <span key={t.name} className="inline-flex items-center gap-1 px-2 py-1 bg-yellow-100 text-yellow-800 rounded text-xs">
                        {t.name} <code className="text-yellow-600">{t.hash}</code>
                      </span>
                    ))}
                  </div>
                ) : <p className="text-sm text-gray-400">No tags</p>}
              </div>
            </>
          )}
        </div>
      )}

      {/* ====== Issues & PRs tab ====== */}
      {tab === 'issues' && (
        <div className="space-y-6">
          {/* Create task from prompt */}
          <div className="bg-white rounded-lg shadow p-6">
            <div className="flex justify-between items-center mb-4">
              <h2 className="text-lg font-semibold">Create Task from Prompt</h2>
              <button onClick={() => setShowPromptForm(!showPromptForm)} className="text-sm text-blue-600">
                {showPromptForm ? 'Cancel' : '+ New Task'}
              </button>
            </div>
            {showPromptForm && (
              <div className="space-y-3">
                <input value={promptTitle} onChange={(e) => setPromptTitle(e.target.value)}
                  placeholder="Issue title" className="w-full px-3 py-2 border rounded text-sm" />
                <div className="flex gap-2 mb-1">
                  <button onClick={() => setPromptPreview(false)}
                    className={`text-xs px-2 py-1 rounded ${!promptPreview ? 'bg-gray-200' : 'text-gray-500'}`}>Edit</button>
                  <button onClick={() => setPromptPreview(true)}
                    className={`text-xs px-2 py-1 rounded ${promptPreview ? 'bg-gray-200' : 'text-gray-500'}`}>Preview</button>
                </div>
                {promptPreview ? (
                  <div className="min-h-[200px] px-3 py-2 border rounded text-sm prose prose-sm max-w-none bg-gray-50">
                    <Markdown>{promptBody || '*No content*'}</Markdown>
                  </div>
                ) : (
                  <textarea value={promptBody} onChange={(e) => setPromptBody(e.target.value)}
                    placeholder="Describe the task in Markdown..." rows={8}
                    className="w-full px-3 py-2 border rounded text-sm font-mono" />
                )}
                <button onClick={() => createFromPrompt.mutate()}
                  disabled={!promptTitle.trim() || !promptBody.trim() || createFromPrompt.isPending}
                  className="px-4 py-2 bg-blue-600 text-white rounded text-sm disabled:opacity-50">
                  {createFromPrompt.isPending ? 'Creating...' : 'Create Issue & Task'}
                </button>
              </div>
            )}
          </div>

          {/* Open Issues */}
          <div className="bg-white rounded-lg shadow p-6">
            <div className="flex justify-between items-center mb-4">
              <h2 className="text-lg font-semibold">Open Issues</h2>
              <button onClick={() => { refetchIssues(); refetchPRs() }}
                className="px-3 py-1 text-xs border rounded hover:bg-gray-50">
                Refresh
              </button>
            </div>
            {issuesLoading ? <Loading /> : issues && issues.length > 0 ? (
              <div className="space-y-2">
                {issues.map((issue) => (
                  <div key={issue.number} className="flex justify-between items-start py-2 px-3 bg-gray-50 rounded">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <a href={`${project.repo_url}/issues/${issue.number}`} target="_blank" rel="noopener noreferrer"
                          className="text-sm font-medium text-blue-600 hover:underline">#{issue.number}</a>
                        <span className="text-sm text-gray-800">{issue.title}</span>
                      </div>
                      <div className="flex items-center gap-2 mt-1">
                        <span className="text-xs text-gray-400">by @{issue.user}</span>
                        {issue.labels?.map((l) => (
                          <span key={l} className="px-1.5 py-0.5 bg-blue-100 text-blue-700 rounded text-xs">{l}</span>
                        ))}
                      </div>
                    </div>
                    <button onClick={() => createTaskForIssue.mutate(issue.number)}
                      disabled={createTaskForIssue.isPending}
                      className="ml-3 px-2 py-1 text-xs bg-blue-600 text-white rounded hover:bg-blue-700 shrink-0 disabled:opacity-50">
                      Run Task
                    </button>
                  </div>
                ))}
              </div>
            ) : <p className="text-sm text-gray-500">No open issues</p>}
          </div>

          {/* Open PRs */}
          <div className="bg-white rounded-lg shadow p-6">
            <h2 className="text-lg font-semibold mb-4">Open Pull Requests</h2>
            {prsLoading ? <Loading /> : prs && prs.length > 0 ? (
              <div className="space-y-2">
                {prs.map((pr) => (
                  <div key={pr.number} className="py-2 px-3 bg-gray-50 rounded">
                    <a href={pr.html_url} target="_blank" rel="noopener noreferrer" className="text-sm text-blue-600 hover:underline">
                      #{pr.number} {pr.title}
                    </a>
                    <span className="ml-2 text-xs text-gray-500">{pr.head} &rarr; {pr.base}</span>
                  </div>
                ))}
              </div>
            ) : <p className="text-sm text-gray-500">No open pull requests</p>}
          </div>
        </div>
      )}

      {/* ====== Tasks tab ====== */}
      {tab === 'tasks' && (
        <div className="bg-white rounded-lg shadow overflow-hidden">
          {tasksLoading ? <Loading /> : (
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">ID</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Issue</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Type</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {(tasks as Task[])?.map((task) => (
                  <tr key={task.id} className="hover:bg-gray-50">
                    <td className="px-6 py-4"><Link to={`/tasks/${task.id}`} className="text-blue-600 hover:underline">#{task.id}</Link></td>
                    <td className="px-6 py-4 text-sm">
                      <a href={`${project.repo_url}/issues/${task.issue_number}`} target="_blank" rel="noopener noreferrer" className="text-blue-600 hover:underline">#{task.issue_number}</a>
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-600">{task.type}</td>
                    <td className="px-6 py-4"><StatusBadge status={task.status} /></td>
                    <td className="px-6 py-4 text-sm text-gray-500">{new Date(task.created_at).toLocaleString()}</td>
                  </tr>
                ))}
                {(!tasks || tasks.length === 0) && (
                  <tr><td colSpan={5} className="px-6 py-8 text-center text-gray-500">No tasks for this project</td></tr>
                )}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* ====== Prompt Templates tab ====== */}
      {tab === 'templates' && (
        <div className="bg-white rounded-lg shadow p-6">
          <div className="flex justify-between items-center mb-4">
            <h2 className="text-lg font-semibold">Prompt Templates</h2>
            <button onClick={() => setShowTemplateForm(!showTemplateForm)} className="text-sm text-blue-600">{showTemplateForm ? 'Cancel' : '+ New'}</button>
          </div>
          {showTemplateForm && (
            <div className="mb-4 space-y-2 p-4 bg-gray-50 rounded">
              <input value={newTemplate.name} onChange={(e) => setNewTemplate({...newTemplate, name: e.target.value})} placeholder="Template name" className="w-full px-3 py-1.5 border rounded text-sm" />
              <textarea value={newTemplate.system_prompt} onChange={(e) => setNewTemplate({...newTemplate, system_prompt: e.target.value})} placeholder="System prompt" rows={3} className="w-full px-3 py-1.5 border rounded text-sm" />
              <textarea value={newTemplate.task_prompt} onChange={(e) => setNewTemplate({...newTemplate, task_prompt: e.target.value})} placeholder="Task prompt" rows={3} className="w-full px-3 py-1.5 border rounded text-sm" />
              <button onClick={() => createTemplate.mutate()} className="px-3 py-1.5 bg-blue-600 text-white rounded text-sm">Create</button>
            </div>
          )}
          {templatesLoading ? <Loading /> : templates && templates.length > 0 ? (
            <div className="space-y-2">
              {templates.map((t) => (
                <div key={t.id} className="flex justify-between items-center py-2 px-3 bg-gray-50 rounded">
                  <span className="text-sm font-medium">{t.name} {t.is_builtin && <span className="text-xs text-gray-400">(builtin)</span>}</span>
                  {!t.is_builtin && <button onClick={() => deleteTemplate.mutate(t.id)} className="text-red-500 text-xs hover:underline">Delete</button>}
                </div>
              ))}
            </div>
          ) : !showTemplateForm && <p className="text-sm text-gray-500">No templates yet.</p>}
        </div>
      )}
    </div>
  )
}
