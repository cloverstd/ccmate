const BASE_URL = '/api'

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...options?.headers },
  })
  if (!res.ok) {
    const error = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(error.error || 'Request failed')
  }
  return res.json()
}

// Auth
export interface CurrentUser { user: string; has_passkey: boolean }

export const authApi = {
  me: () => request<CurrentUser>('/auth/me'),
  checkSession: async (): Promise<CurrentUser | null> => {
    try { return await request<CurrentUser>('/auth/me') } catch { return null }
  },
  logout: () => fetch(`${BASE_URL}/auth/logout`, { method: 'POST' }),
  passkeyRegisterStart: () => request<unknown>('/auth/passkey/register/start', { method: 'POST' }),
  passkeyRegisterFinish: (body: unknown) => request<unknown>('/auth/passkey/register/finish', { method: 'POST', body: JSON.stringify(body) }),
  passkeyRemove: () => request<unknown>('/auth/passkey', { method: 'DELETE' }),
  passkeyLoginStart: (username: string) => fetch(`${BASE_URL}/auth/passkey/login/start`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username }),
  }),
  passkeyLoginFinish: (username: string, body: unknown) => fetch(`${BASE_URL}/auth/passkey/login/finish`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username, response_data: body }),
  }),
}

// Settings
export const settingsApi = {
  get: () => request<Record<string, string>>('/settings'),
  update: (data: Record<string, string>) => request<{ status: string }>('/settings', { method: 'PUT', body: JSON.stringify(data) }),
  checkGitHubPermissions: () => request<{ configured: boolean; valid?: boolean; user?: string; scopes?: string; error?: string }>('/github/permissions'),
  testNotification: () => request<{ status: string }>('/notifications/test', { method: 'POST' }),
}

// Projects
export interface Project {
  id: number; name: string; repo_url: string; git_provider: string
  default_branch: string; auto_mode: boolean; default_agent_profile_id: number | null; default_prompt_template_id: number | null
  prompt_template_scope: 'global_only' | 'project_only' | 'merged'
  created_at: string; updated_at: string
}

export interface GitHubRepo {
  full_name: string; html_url: string; default_branch: string; description: string; private: boolean
}

export interface LabelRuleConfig { label: string; trigger_mode: string }
export interface RepoIssue { number: number; title: string; body: string; labels: string[]; state: string; user: string; created_at: string; updated_at: string }
export interface CheckRun { name: string; status: string; conclusion: string }
export interface RepoPR { number: number; title: string; body: string; state: string; mergeable?: boolean | null; user: string; html_url: string; head: string; base: string; check_status?: string; check_details?: CheckRun[]; created_at: string; updated_at: string }
export interface PromptTemplate { id: number; name: string; system_prompt: string; task_prompt: string; is_builtin: boolean; project_id: number | null; created_at: string }
export interface AgentProfile { id: number; provider: string; model: string; supports_image: boolean; supports_resume: boolean; config_json: string }

export const githubApi = { listRepos: () => request<GitHubRepo[]>('/github/repos') }

export interface BranchInfo { name: string; hash: string; message?: string; current?: boolean }
export interface CommitInfo { hash: string; message: string; author: string; date: string }
export interface TagInfo { name: string; hash: string }
export interface RepoGitInfo { project_id: number; repo_path: string; branches: BranchInfo[] | null; tags: TagInfo[] | null }

export const projectsApi = {
  list: () => request<Project[]>('/projects'),
  get: (id: number) => request<Project>(`/projects/${id}`),
  create: (data: Partial<Project>) => request<Project>('/projects', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: number, data: Partial<Project>) => request<Project>(`/projects/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  listTasks: (id: number) => request<Task[]>(`/projects/${id}/tasks`),
  listIssues: (id: number) => request<RepoIssue[]>(`/projects/${id}/issues`),
  listPRs: (id: number) => request<RepoPR[]>(`/projects/${id}/pulls`),
  gitInfo: (id: number) => request<RepoGitInfo>(`/projects/${id}/git-info`),
  commits: (id: number, branch: string, limit = 20) => request<CommitInfo[]>(`/projects/${id}/commits?branch=${encodeURIComponent(branch)}&limit=${limit}`),
  pull: (id: number) => request<{ status: string }>(`/projects/${id}/pull`, { method: 'POST' }),
}

export const labelRulesApi = { list: () => request<LabelRuleConfig[]>('/label-rules') }

export const promptsApi = {
  list: (params?: { scope?: 'global' | 'project' | 'all'; project_id?: number }) => {
    const qs = new URLSearchParams()
    if (params?.scope) qs.set('scope', params.scope)
    if (params?.project_id) qs.set('project_id', String(params.project_id))
    const query = qs.toString()
    return request<PromptTemplate[]>(`/prompt-templates${query ? `?${query}` : ''}`)
  },
  create: (data: Partial<PromptTemplate>) => request<PromptTemplate>('/prompt-templates', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: number, data: Partial<PromptTemplate>) => request<PromptTemplate>(`/prompt-templates/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  delete: (id: number) => request<void>(`/prompt-templates/${id}`, { method: 'DELETE' }),
}

export const modelsApi = {
  list: () => request<AgentProfile[]>('/models'),
  create: (data: Partial<AgentProfile>) => request<AgentProfile>('/models', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: number, data: Partial<AgentProfile>) => request<AgentProfile>(`/models/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  delete: (id: number) => request<void>(`/models/${id}`, { method: 'DELETE' }),
}

// Tasks
export type TaskStatus = 'pending' | 'queued' | 'running' | 'paused' | 'waiting_user' | 'succeeded' | 'failed' | 'cancelled'
export type TaskType = 'issue_implementation' | 'review_fix' | 'manual_followup'
export interface Task {
  id: number; issue_number: number; pr_number: number | null; type: TaskType
  status: TaskStatus; priority: number; trigger_source: string; agent_profile_id: number | null
  current_session_id: number | null; created_at: string; updated_at: string
  edges: { project?: Project; sessions?: Session[]; prompt_snapshot?: PromptSnapshot | null }
}
export interface Session { id: number; status: string; started_at: string | null; ended_at: string | null; edges: { messages?: SessionMessage[]; events?: SessionEvent[] } }
export interface SessionMessage { id: number; role: string; content_type: string; content: string; sequence: number; created_at: string }
export interface SessionEvent { id: number; event_type: string; payload_json: string; sequence: number; created_at: string }

export interface TaskGitSummary { branch: string; latest_commit?: CommitInfo | null; branches?: BranchInfo[] | null }
export interface PromptSnapshot { id: number; system_prompt: string; task_prompt: string; model_name: string; model_version: string; created_at: string }
export interface TaskDetail {
  task: Task; workspace_path: string; issue?: RepoIssue | null; pull_request?: RepoPR | null
  git?: TaskGitSummary | null; agent_profile?: AgentProfile | null
}

export const tasksApi = {
  list: (params?: { status?: string; project_id?: string }) => {
    const query = new URLSearchParams(params as Record<string, string>).toString()
    return request<Task[]>(`/tasks${query ? '?' + query : ''}`)
  },
  get: (id: number) => request<TaskDetail>(`/tasks/${id}`),
  create: (data: { project_id: number; issue_number: number; type?: string; agent_profile_id?: number }) =>
    request<Task>('/tasks', { method: 'POST', body: JSON.stringify(data) }),
  createFromPrompt: (data: { project_id: number; title: string; body: string; labels?: string[]; agent_profile_id?: number }) =>
    request<{ issue: RepoIssue; task: Task }>('/tasks/from-prompt', { method: 'POST', body: JSON.stringify(data) }),
  pause: (id: number) => request<void>(`/tasks/${id}/pause`, { method: 'POST' }),
  resume: (id: number) => request<void>(`/tasks/${id}/resume`, { method: 'POST' }),
  retry: (id: number) => request<void>(`/tasks/${id}/retry`, { method: 'POST' }),
  cancel: (id: number) => request<void>(`/tasks/${id}/cancel`, { method: 'POST' }),
  complete: (id: number, data: { close_issue: boolean; merge_pr: boolean }) => request<{ status: string; actions: string[] }>(`/tasks/${id}/complete`, { method: 'POST', body: JSON.stringify(data) }),
  sendMessage: (id: number, content: string) => request<SessionMessage>(`/tasks/${id}/messages`, {
    method: 'POST', body: JSON.stringify({ content, content_type: 'text' }),
  }),
  uploadAttachment: async (id: number, file: File) => {
    const formData = new FormData()
    formData.append('file', file)
    const res = await fetch(`${BASE_URL}/tasks/${id}/attachments`, { method: 'POST', body: formData })
    if (!res.ok) throw new Error('Upload failed')
    return res.json()
  },
}

export function subscribeToTaskEvents(taskId: number, onEvent: (event: { type: string; data: unknown }) => void) {
  const eventSource = new EventSource(`${BASE_URL}/tasks/${taskId}/events/stream`)
  eventSource.onmessage = (e) => { try { onEvent({ type: e.type || 'message', data: JSON.parse(e.data) }) } catch {} }
  eventSource.addEventListener('connected', () => onEvent({ type: 'connected', data: {} }))
  for (const type of ['message.delta','message.completed','tool.call','tool.result','run.status','task.completed','task.failed','message.created']) {
    eventSource.addEventListener(type, (e) => { try { onEvent({ type, data: JSON.parse(e.data) }) } catch {} })
  }
  return () => eventSource.close()
}
