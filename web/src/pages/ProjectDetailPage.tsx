import { useParams, Link, useLocation, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import Markdown from '../components/Markdown'
import PromptEditor from '../components/PromptEditor'
import { projectsApi, tasksApi, promptsApi, modelsApi, type Project, type Task, type CommitInfo, type AgentProfile } from '../lib/api'
import StatusBadge from '../components/StatusBadge'
import { Card, CardHeader, CardContent, CardFooter, Label, Input, Select, Checkbox, Btn, Tag, EmptyState } from '../components/ui'
import { useToast } from '../components/Toast'

type Tab = 'info' | 'git' | 'issues' | 'tasks' | 'templates'

export default function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>()
  const projectId = parseInt(id || '0')
  const { toast } = useToast()
  const queryClient = useQueryClient()
  const location = useLocation()
  const navigate = useNavigate()
  const tab = (location.hash.replace('#', '') as Tab) || 'info'
  const setTab = (t: Tab) => navigate({ hash: t }, { replace: true })

  const [showPromptForm, setShowPromptForm] = useState(false)
  const [promptTitle, setPromptTitle] = useState('')
  const [promptBody, setPromptBody] = useState('')
  const [promptPreview, setPromptPreview] = useState(false)
  const [projectForm, setProjectForm] = useState({ default_branch: 'main', auto_mode: false, default_agent_profile_id: '', default_prompt_template_id: '', prompt_template_scope: 'project_only' as string })
  const [taskAgentProfileID, setTaskAgentProfileID] = useState('')

  const { data: project, isLoading } = useQuery({ queryKey: ['project', projectId], queryFn: () => projectsApi.get(projectId), enabled: projectId > 0 })
  const { data: models } = useQuery({ queryKey: ['models'], queryFn: modelsApi.list, enabled: projectId > 0 })
  const [selectedBranch, setSelectedBranch] = useState<string>('')
  const { data: gitInfo, isLoading: gitLoading, refetch: refetchGit } = useQuery({
    queryKey: ['project-git', projectId], queryFn: () => projectsApi.gitInfo(projectId), enabled: projectId > 0 && tab === 'git',
  })
  const { data: commits, isLoading: commitsLoading } = useQuery({
    queryKey: ['project-commits', projectId, selectedBranch],
    queryFn: () => projectsApi.commits(projectId, selectedBranch || 'HEAD'),
    enabled: projectId > 0 && tab === 'git' && !!gitInfo,
  })
  const pullMutation = useMutation({ mutationFn: () => projectsApi.pull(projectId), onSuccess: () => refetchGit() })

  const { data: tasks, isLoading: tasksLoading } = useQuery({ queryKey: ['project-tasks', projectId], queryFn: () => projectsApi.listTasks(projectId), enabled: projectId > 0 })
  const { data: issues, isLoading: issuesLoading, refetch: refetchIssues } = useQuery({
    queryKey: ['project-issues', projectId], queryFn: () => projectsApi.listIssues(projectId), enabled: projectId > 0 && tab === 'issues',
  })
  const { data: prs, isLoading: prsLoading, refetch: refetchPRs } = useQuery({
    queryKey: ['project-prs', projectId], queryFn: () => projectsApi.listPRs(projectId), enabled: projectId > 0 && tab === 'issues',
  })
  const { data: templates, isLoading: templatesLoading } = useQuery({
    queryKey: ['prompt-templates', projectId], queryFn: () => promptsApi.list({ scope: 'all', project_id: projectId }), enabled: projectId > 0,
  })
  const [newTemplate, setNewTemplate] = useState({ name: '', system_prompt: '', task_prompt: '' })
  const [showTemplateForm, setShowTemplateForm] = useState(false)
  const [editingProjTemplateID, setEditingProjTemplateID] = useState<number | null>(null)

  const buildProjectPayload = (overrides?: Record<string, string | boolean>) => {
    const form = { ...projectForm, ...overrides }
    return {
      ...project, default_branch: form.default_branch, auto_mode: form.auto_mode,
      default_agent_profile_id: form.default_agent_profile_id ? parseInt(form.default_agent_profile_id as string, 10) : null,
      default_prompt_template_id: form.default_prompt_template_id ? parseInt(form.default_prompt_template_id as string, 10) : null,
      prompt_template_scope: form.prompt_template_scope as 'global_only' | 'project_only' | 'merged',
    }
  }
  const updateProject = useMutation({
    mutationFn: (payload: Partial<Project>) => projectsApi.update(projectId, payload),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['project', projectId] }); queryClient.invalidateQueries({ queryKey: ['projects'] }) },
  })
  const createTemplate = useMutation({
    mutationFn: () => promptsApi.create({ ...newTemplate, project_id: projectId }),
    onSuccess: async (template) => {
      const nf = { ...projectForm, default_prompt_template_id: String(template.id) }
      setProjectForm(nf)
      await updateProject.mutateAsync(buildProjectPayload({ default_prompt_template_id: String(template.id) }))
      queryClient.invalidateQueries({ queryKey: ['prompt-templates', projectId] })
      setShowTemplateForm(false); setNewTemplate({ name: '', system_prompt: '', task_prompt: '' })
    },
  })
  const updateTemplate = useMutation({
    mutationFn: () => promptsApi.update(editingProjTemplateID!, newTemplate),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['prompt-templates', projectId] }); setEditingProjTemplateID(null); setShowTemplateForm(false); setNewTemplate({ name: '', system_prompt: '', task_prompt: '' }) },
  })
  const deleteTemplate = useMutation({
    mutationFn: (tid: number) => promptsApi.delete(tid),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['prompt-templates', projectId] }),
  })

  const createFromPrompt = useMutation({
    mutationFn: () => tasksApi.createFromPrompt({ project_id: projectId, title: promptTitle, body: promptBody, agent_profile_id: taskAgentProfileID ? parseInt(taskAgentProfileID, 10) : undefined }),
    onSuccess: (result) => { queryClient.invalidateQueries({ queryKey: ['project-tasks'] }); queryClient.invalidateQueries({ queryKey: ['project-issues'] }); setShowPromptForm(false); setPromptTitle(''); setPromptBody(''); if (result.task?.id) navigate(`/tasks/${result.task.id}`) },
  })
  const createTaskForIssue = useMutation({
    mutationFn: (issueNumber: number) => tasksApi.create({ project_id: projectId, issue_number: issueNumber, agent_profile_id: taskAgentProfileID ? parseInt(taskAgentProfileID, 10) : undefined }),
    onSuccess: (task) => { queryClient.invalidateQueries({ queryKey: ['project-tasks'] }); navigate(`/tasks/${task.id}`) },
    onError: (err) => { queryClient.invalidateQueries({ queryKey: ['project-tasks'] }); toast(err.message, 'error') },
  })

  useEffect(() => {
    if (!project) return
    const pd = project.default_agent_profile_id != null ? String(project.default_agent_profile_id) : ''
    setProjectForm({
      default_branch: project.default_branch, auto_mode: project.auto_mode,
      default_agent_profile_id: pd,
      default_prompt_template_id: project.default_prompt_template_id != null ? String(project.default_prompt_template_id) : '',
      prompt_template_scope: project.prompt_template_scope || 'project_only',
    })
    setTaskAgentProfileID(pd)
  }, [project])

  if (isLoading) return <div className="py-8 text-center text-gray-400 text-sm">Loading...</div>
  if (!project) return <div className="text-gray-500">Project not found</div>

  const tabs: { key: Tab; label: string }[] = [
    { key: 'info', label: 'Info' }, { key: 'git', label: 'Git' },
    { key: 'issues', label: 'Issues & PRs' }, { key: 'tasks', label: 'Tasks' },
    { key: 'templates', label: 'Prompts' },
  ]

  return (
    <div>
      <div className="flex items-center gap-3 mb-4">
        <h1 className="text-2xl font-bold">{project.name}</h1>
        <a href={project.repo_url} target="_blank" rel="noopener noreferrer" className="text-gray-400 hover:text-gray-600" title="Open on GitHub">
          <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/></svg>
        </a>
      </div>

      <div className="border-b border-gray-200 mb-6">
        <div className="flex gap-4">
          {tabs.map((t) => (
            <button key={t.key} onClick={() => setTab(t.key)}
              className={`pb-2 text-sm font-medium ${tab === t.key ? 'border-b-2 border-blue-600 text-blue-600' : 'text-gray-500 hover:text-gray-700'}`}>{t.label}</button>
          ))}
        </div>
      </div>

      {/* ====== Info ====== */}
      {tab === 'info' && (
        <Card>
          <CardHeader title="Project Settings" />
          <CardContent>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-5">
              <div>
                <Label>Repository</Label>
                <a href={project.repo_url} target="_blank" rel="noopener noreferrer" className="text-sm text-blue-600 hover:underline">{project.repo_url}</a>
              </div>
              <div>
                <Label>Default Branch</Label>
                <p className="text-sm text-gray-700 font-mono mt-1">{project.default_branch}</p>
              </div>
              <div>
                <Checkbox label={projectForm.auto_mode ? 'Automatic' : 'Manual'} checked={projectForm.auto_mode}
                  onChange={(v) => setProjectForm({ ...projectForm, auto_mode: v })}
                  description="Automatically create tasks when issue labels match" />
              </div>
              <div>
                <Label>Default Agent</Label>
                <Select value={projectForm.default_agent_profile_id} onChange={(e) => setProjectForm({ ...projectForm, default_agent_profile_id: e.target.value })} className="w-full">
                  <option value="">Use global default</option>
                  {(models || []).map((m: AgentProfile) => <option key={m.id} value={String(m.id)}>{m.provider} / {m.model}</option>)}
                </Select>
              </div>
              <div>
                <Label>Template Scope</Label>
                <Select value={projectForm.prompt_template_scope} onChange={(e) => setProjectForm({ ...projectForm, prompt_template_scope: e.target.value })} className="w-full">
                  <option value="project_only">Project Only</option>
                  <option value="global_only">Global Only</option>
                  <option value="merged">Merged</option>
                </Select>
              </div>
              {projectForm.prompt_template_scope !== 'global_only' && (
                <div>
                  <Label>Project Template</Label>
                  <Select value={projectForm.default_prompt_template_id} onChange={(e) => setProjectForm({ ...projectForm, default_prompt_template_id: e.target.value })} className="w-full">
                    <option value="">Platform default</option>
                    {(templates || []).filter(t => t.project_id === projectId || t.project_id == null).map((t) => (
                      <option key={t.id} value={String(t.id)}>{t.name}{t.project_id == null ? ' (global)' : ''}</option>
                    ))}
                  </Select>
                </div>
              )}
            </div>
          </CardContent>
          <CardFooter>
            <Btn onClick={() => updateProject.mutate(buildProjectPayload())} disabled={updateProject.isPending}>
              {updateProject.isPending ? 'Saving...' : 'Save Settings'}
            </Btn>
          </CardFooter>
        </Card>
      )}

      {/* ====== Git ====== */}
      {tab === 'git' && (
        <div className="space-y-6">
          <Card>
            <CardHeader title="Repository" description={gitInfo?.repo_path} action={
              <div className="flex gap-2">
                <Btn variant="secondary" size="sm" onClick={() => pullMutation.mutate()} disabled={pullMutation.isPending}>
                  {pullMutation.isPending ? 'Fetching...' : 'Fetch'}
                </Btn>
                <Btn variant="ghost" size="sm" onClick={() => refetchGit()}>Refresh</Btn>
              </div>
            } />
          </Card>

          {gitLoading ? <div className="py-8 text-center text-gray-400 text-sm">Loading...</div> : (
            <>
              <Card>
                <CardHeader title={`Branches (${gitInfo?.branches?.length || 0})`} />
                <CardContent className="!py-2">
                  <div className="divide-y divide-gray-100">
                    {(gitInfo?.branches || []).map((b) => (
                      <button key={b.name} onClick={() => setSelectedBranch(b.name)}
                        className={`w-full text-left flex items-center gap-3 py-2.5 px-3 rounded-lg text-sm hover:bg-gray-50 transition-colors ${selectedBranch === b.name ? 'bg-blue-50 ring-1 ring-blue-200' : ''}`}>
                        <span className="font-medium shrink-0">{b.name}</span>
                        <code className="text-xs text-gray-400 shrink-0">{b.hash}</code>
                        <span className="text-xs text-gray-500 truncate">{b.message || ''}</span>
                      </button>
                    ))}
                    {(!gitInfo?.branches || gitInfo.branches.length === 0) && <EmptyState>No branches found</EmptyState>}
                  </div>
                </CardContent>
              </Card>

              {selectedBranch && (
                <Card>
                  <CardHeader title={`Commits on ${selectedBranch}`} />
                  <CardContent className="!py-2">
                    {commitsLoading ? <div className="py-4 text-center text-gray-400 text-sm">Loading...</div> : (
                      <div className="divide-y divide-gray-100">
                        {(commits as CommitInfo[] || []).map((c, i) => (
                          <div key={i} className="flex items-start gap-3 py-2.5 px-3 text-sm">
                            <code className="text-xs text-blue-600 shrink-0 mt-0.5">{c.hash}</code>
                            <div className="flex-1 min-w-0">
                              <div className="truncate text-gray-900">{c.message}</div>
                              <div className="text-xs text-gray-400">{c.author} &middot; {c.date}</div>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </CardContent>
                </Card>
              )}
            </>
          )}
        </div>
      )}

      {/* ====== Issues & PRs ====== */}
      {tab === 'issues' && (
        <div className="space-y-6">
          <Card>
            <CardHeader title="Create Task from Prompt" action={
              <Btn variant="ghost" size="sm" onClick={() => setShowPromptForm(!showPromptForm)}>{showPromptForm ? 'Cancel' : '+ New Task'}</Btn>
            } />
            {showPromptForm && (
              <CardContent>
                <div>
                  <Label>Agent</Label>
                  <Select value={taskAgentProfileID} onChange={(e) => setTaskAgentProfileID(e.target.value)} className="w-full max-w-md">
                    <option value="">Use default</option>
                    {(models || []).map((m: AgentProfile) => <option key={m.id} value={String(m.id)}>{m.provider} / {m.model}</option>)}
                  </Select>
                </div>
                <Input value={promptTitle} onChange={(e) => setPromptTitle(e.target.value)} placeholder="Issue title" />
                <div className="flex gap-2 mb-1">
                  <Btn variant={!promptPreview ? 'secondary' : 'ghost'} size="sm" onClick={() => setPromptPreview(false)}>Edit</Btn>
                  <Btn variant={promptPreview ? 'secondary' : 'ghost'} size="sm" onClick={() => setPromptPreview(true)}>Preview</Btn>
                </div>
                {promptPreview ? (
                  <div className="min-h-[200px] px-3 py-2 border rounded-lg text-sm prose prose-sm max-w-none bg-gray-50">
                    <Markdown>{promptBody || '*No content*'}</Markdown>
                  </div>
                ) : (
                  <textarea value={promptBody} onChange={(e) => setPromptBody(e.target.value)} placeholder="Describe the task in Markdown..." rows={8}
                    className="w-full px-3 py-2 rounded-lg border border-gray-300 text-sm font-mono shadow-sm focus:outline-none focus:ring-2 focus:ring-blue-500/20 focus:border-blue-500" />
                )}
                <Btn onClick={() => createFromPrompt.mutate()} disabled={!promptTitle.trim() || !promptBody.trim() || createFromPrompt.isPending}>
                  {createFromPrompt.isPending ? 'Creating...' : 'Create Issue & Task'}
                </Btn>
              </CardContent>
            )}
          </Card>

          <Card>
            <CardHeader title="Open Issues" action={
              <div className="flex items-center gap-2">
                <Select value={taskAgentProfileID} onChange={(e) => setTaskAgentProfileID(e.target.value)} className="text-xs">
                  <option value="">Default agent</option>
                  {(models || []).map((m: AgentProfile) => <option key={m.id} value={String(m.id)}>{m.provider}/{m.model}</option>)}
                </Select>
                <Btn variant="ghost" size="sm" onClick={() => { refetchIssues(); refetchPRs() }}>Refresh</Btn>
              </div>
            } />
            <CardContent className="!py-2">
              {issuesLoading ? <div className="py-4 text-center text-gray-400 text-sm">Loading...</div> : issues && issues.length > 0 ? (
                <div className="divide-y divide-gray-100">
                  {issues.map((issue) => {
                    const activeTask = (tasks || []).find((t: Task) => t.issue_number === issue.number && ['queued','running','paused','waiting_user'].includes(t.status))
                    return (
                      <div key={issue.number} className="flex justify-between items-start py-3 px-3">
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-2">
                            <a href={`${project.repo_url}/issues/${issue.number}`} target="_blank" rel="noopener noreferrer" className="text-sm font-medium text-blue-600 hover:underline">#{issue.number}</a>
                            <span className="text-sm text-gray-800">{issue.title}</span>
                          </div>
                          <div className="flex items-center gap-2 mt-1">
                            <span className="text-xs text-gray-400">@{issue.user}</span>
                            {issue.labels?.map((l) => <Tag key={l}>{l}</Tag>)}
                          </div>
                        </div>
                        {activeTask ? (
                          <Link to={`/tasks/${activeTask.id}`}>
                            <Tag color="yellow">Task #{activeTask.id} ({activeTask.status})</Tag>
                          </Link>
                        ) : (
                          <Btn size="sm" onClick={() => createTaskForIssue.mutate(issue.number)} disabled={createTaskForIssue.isPending}>Run</Btn>
                        )}
                      </div>
                    )
                  })}
                </div>
              ) : <EmptyState>No open issues</EmptyState>}
            </CardContent>
          </Card>

          <Card>
            <CardHeader title="Open Pull Requests" />
            <CardContent className="!py-2">
              {prsLoading ? <div className="py-4 text-center text-gray-400 text-sm">Loading...</div> : prs && prs.length > 0 ? (
                <div className="divide-y divide-gray-100">
                  {prs.map((pr) => (
                    <div key={pr.number} className="py-3 px-3 flex items-center gap-3">
                      <a href={pr.html_url} target="_blank" rel="noopener noreferrer" className="text-sm text-blue-600 hover:underline font-medium">#{pr.number}</a>
                      <span className="text-sm text-gray-800">{pr.title}</span>
                      <span className="text-xs text-gray-400 ml-auto">{pr.head} &rarr; {pr.base}</span>
                    </div>
                  ))}
                </div>
              ) : <EmptyState>No open pull requests</EmptyState>}
            </CardContent>
          </Card>
        </div>
      )}

      {/* ====== Tasks ====== */}
      {tab === 'tasks' && (
        <Card>
          <CardHeader title="Tasks" />
          <CardContent className="!p-0">
            {tasksLoading ? <div className="py-8 text-center text-gray-400 text-sm">Loading...</div> : (
              <div className="divide-y divide-gray-100">
                {(tasks as Task[])?.map((task) => (
                  <Link key={task.id} to={`/tasks/${task.id}`} className="block px-4 py-3 hover:bg-gray-50 transition-colors">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-sm font-medium text-blue-600">#{task.id}</span>
                      <span className="text-xs text-gray-500">Issue #{task.issue_number}</span>
                      <StatusBadge status={task.status} />
                      <span className="text-xs text-gray-400 ml-auto hidden sm:block">{new Date(task.created_at).toLocaleString()}</span>
                    </div>
                  </Link>
                ))}
                {(!tasks || tasks.length === 0) && (
                  <div className="px-6 py-8 text-center text-gray-400 text-sm">No tasks yet</div>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* ====== Templates ====== */}
      {tab === 'templates' && (
        <div className="space-y-6">
          <Card>
            <CardHeader title="Template Scope" />
            <CardContent>
              <div className="flex flex-wrap gap-2">
                {([{ value: 'project_only', label: 'Project Only' }, { value: 'global_only', label: 'Global Only' }, { value: 'merged', label: 'Merged' }] as const).map((opt) => (
                  <Btn key={opt.value}
                    variant={projectForm.prompt_template_scope === opt.value ? 'primary' : 'secondary'} size="sm"
                    onClick={() => { setProjectForm(prev => ({ ...prev, prompt_template_scope: opt.value })); updateProject.mutate(buildProjectPayload({ prompt_template_scope: opt.value })) }}>
                    {opt.label}
                  </Btn>
                ))}
              </div>
            </CardContent>
          </Card>

          {projectForm.prompt_template_scope !== 'project_only' && (
            <Card>
              <CardHeader title="Global Templates" />
              <CardContent className="!py-2">
                {templatesLoading ? <div className="py-4 text-center text-gray-400 text-sm">Loading...</div> : (() => {
                  const gt = (templates || []).filter(t => t.project_id == null)
                  return gt.length > 0 ? (
                    <div className="divide-y divide-gray-100">
                      {gt.map((t) => (
                        <div key={t.id} className="flex justify-between items-center py-2.5 px-3">
                          <div className="flex items-center gap-2"><span className="text-sm font-medium">{t.name}</span>{t.is_builtin && <Tag color="gray">builtin</Tag>}</div>
                          <span className="text-xs text-gray-400">Managed in Settings</span>
                        </div>
                      ))}
                    </div>
                  ) : <EmptyState>No global templates</EmptyState>
                })()}
              </CardContent>
            </Card>
          )}

          {projectForm.prompt_template_scope !== 'global_only' && (
            <Card>
              <CardHeader title="Project Templates" action={
                <Btn variant="ghost" size="sm" onClick={() => {
                  if (showTemplateForm) { setShowTemplateForm(false); setEditingProjTemplateID(null); setNewTemplate({ name: '', system_prompt: '', task_prompt: '' }) }
                  else setShowTemplateForm(true)
                }}>{showTemplateForm ? 'Cancel' : '+ New'}</Btn>
              } />
              <CardContent>
                {showTemplateForm && (
                  <div className="p-4 rounded-lg border border-blue-200 bg-blue-50/30 space-y-3">
                    <div><Label>Template Name</Label><Input value={newTemplate.name} onChange={(e) => setNewTemplate({ ...newTemplate, name: e.target.value })} placeholder="Template name" /></div>
                    <PromptEditor label="System Prompt" value={newTemplate.system_prompt} onChange={(v) => setNewTemplate({ ...newTemplate, system_prompt: v })} placeholder="System prompt..." rows={4} />
                    <PromptEditor label="Task Prompt Template" value={newTemplate.task_prompt} onChange={(v) => setNewTemplate({ ...newTemplate, task_prompt: v })} placeholder="Task prompt..." rows={4} showVars />
                    <Btn onClick={() => editingProjTemplateID ? updateTemplate.mutate() : createTemplate.mutate()} disabled={!newTemplate.name}>
                      {editingProjTemplateID ? 'Save' : 'Create'}
                    </Btn>
                  </div>
                )}
                {templatesLoading ? <div className="py-4 text-center text-gray-400 text-sm">Loading...</div> : (() => {
                  const pt = (templates || []).filter(t => t.project_id === projectId)
                  return pt.length > 0 ? (
                    <div className="space-y-2">
                      {pt.map((t) => (
                        <div key={t.id} className="flex justify-between items-center p-3 rounded-lg border border-gray-200 hover:border-gray-300 transition-colors">
                          <div className="flex items-center gap-2">
                            <span className="text-sm font-medium">{t.name}</span>
                            {projectForm.default_prompt_template_id === String(t.id) && <Tag color="blue">Active</Tag>}
                          </div>
                          <div className="flex items-center gap-2">
                            {projectForm.default_prompt_template_id !== String(t.id) && (
                              <Btn variant="ghost" size="sm" onClick={() => { setProjectForm(prev => ({ ...prev, default_prompt_template_id: String(t.id) })); updateProject.mutate(buildProjectPayload({ default_prompt_template_id: String(t.id) })) }}>Use</Btn>
                            )}
                            <Btn variant="ghost" size="sm" onClick={() => { setEditingProjTemplateID(t.id); setShowTemplateForm(true); setNewTemplate({ name: t.name, system_prompt: t.system_prompt, task_prompt: t.task_prompt }) }}>Edit</Btn>
                            {!t.is_builtin && <Btn variant="ghost" size="sm" onClick={() => deleteTemplate.mutate(t.id)} className="text-red-500 hover:text-red-700 hover:bg-red-50">Delete</Btn>}
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : !showTemplateForm && <EmptyState>No project templates</EmptyState>
                })()}
              </CardContent>
            </Card>
          )}
        </div>
      )}
    </div>
  )
}
