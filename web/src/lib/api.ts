const BASE_URL = '/api'

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  })
  if (!res.ok) {
    const error = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(error.error || 'Request failed')
  }
  return res.json()
}

// Auth
export interface CurrentUser {
  user: string
  has_passkey: boolean
}

export const authApi = {
  me: () => request<CurrentUser>('/auth/me'),
  checkSession: async (): Promise<CurrentUser | null> => {
    try { return await request<CurrentUser>('/auth/me') } catch { return null }
  },
  logout: () => fetch(`${BASE_URL}/auth/logout`, { method: 'POST' }),

  // Passkey management (post-login)
  passkeyRegisterStart: () => request<unknown>('/auth/passkey/register/start', { method: 'POST' }),
  passkeyRegisterFinish: (body: unknown) => request<unknown>('/auth/passkey/register/finish', {
    method: 'POST', body: JSON.stringify(body),
  }),
  passkeyRemove: () => request<unknown>('/auth/passkey', { method: 'DELETE' }),

  // Passkey login (public)
  passkeyLoginStart: (username: string) => fetch(`${BASE_URL}/auth/passkey/login/start`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username }),
  }),
  passkeyLoginFinish: (username: string, body: unknown) => fetch(`${BASE_URL}/auth/passkey/login/finish`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, response_data: body }),
  }),
}

// Projects
export interface Project {
  id: number; name: string; repo_url: string; git_provider: string
  default_branch: string; auto_mode: boolean; max_concurrency: number
  created_at: string; updated_at: string
  edges?: { label_rules?: LabelRule[] }
}

export interface LabelRule {
  id: number; issue_label: string; trigger_mode: string
}

export interface PromptTemplate {
  id: number; name: string; system_prompt: string; task_prompt: string
  is_builtin: boolean; created_at: string
}

export interface AgentProfile {
  id: number; provider: string; model: string
  supports_image: boolean; supports_resume: boolean; config_json: string
}

export const projectsApi = {
  list: () => request<Project[]>('/projects'),
  get: (id: number) => request<Project>(`/projects/${id}`),
  create: (data: Partial<Project>) => request<Project>('/projects', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: number, data: Partial<Project>) => request<Project>(`/projects/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  listLabelRules: (id: number) => request<LabelRule[]>(`/projects/${id}/label-rules`),
  createLabelRule: (id: number, data: { issue_label: string; trigger_mode: string }) =>
    request<LabelRule>(`/projects/${id}/label-rules`, { method: 'POST', body: JSON.stringify(data) }),
  deleteLabelRule: (id: number) => request<void>(`/label-rules/${id}`, { method: 'DELETE' }),
}

export const promptsApi = {
  list: () => request<PromptTemplate[]>('/prompt-templates'),
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
  status: TaskStatus; priority: number; trigger_source: string
  current_session_id: number | null; created_at: string; updated_at: string
  edges: { project?: Project; sessions?: Session[] }
}

export interface Session {
  id: number; status: string; started_at: string | null; ended_at: string | null
  edges: { messages?: SessionMessage[] }
}

export interface SessionMessage {
  id: number; role: string; content_type: string; content: string
  sequence: number; created_at: string
}

export const tasksApi = {
  list: (params?: { status?: string; project_id?: string }) => {
    const query = new URLSearchParams(params as Record<string, string>).toString()
    return request<Task[]>(`/tasks${query ? '?' + query : ''}`)
  },
  get: (id: number) => request<Task>(`/tasks/${id}`),
  create: (data: { project_id: number; issue_number: number; type?: string }) =>
    request<Task>('/tasks', { method: 'POST', body: JSON.stringify(data) }),
  pause: (id: number) => request<void>(`/tasks/${id}/pause`, { method: 'POST' }),
  resume: (id: number) => request<void>(`/tasks/${id}/resume`, { method: 'POST' }),
  retry: (id: number) => request<void>(`/tasks/${id}/retry`, { method: 'POST' }),
  cancel: (id: number) => request<void>(`/tasks/${id}/cancel`, { method: 'POST' }),
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

// SSE
export function subscribeToTaskEvents(taskId: number, onEvent: (event: { type: string; data: unknown }) => void) {
  const eventSource = new EventSource(`${BASE_URL}/tasks/${taskId}/events/stream`)
  eventSource.onmessage = (e) => {
    try { onEvent({ type: e.type || 'message', data: JSON.parse(e.data) }) } catch {}
  }
  eventSource.addEventListener('connected', () => onEvent({ type: 'connected', data: {} }))
  for (const type of ['message.delta','message.completed','tool.call','tool.result','run.status','task.completed','task.failed','message.created']) {
    eventSource.addEventListener(type, (e) => {
      try { onEvent({ type, data: JSON.parse(e.data) }) } catch {}
    })
  }
  return () => eventSource.close()
}
