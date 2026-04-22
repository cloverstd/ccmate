import { Link, useParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect, useRef, useMemo } from 'react'
import Markdown from '../components/Markdown'
import { tasksApi, subscribeToTaskEvents, type TaskStatus, type SessionMessage, type SessionEvent, type RepoIssue, type RepoPR, type PromptSnapshot } from '../lib/api'
import StatusBadge from '../components/StatusBadge'
import { ToolCallView, ToolResultView, parseToolResult } from '../components/ToolView'
import { Btn, Tag, Card, CardContent } from '../components/ui'
import { useToast } from '../components/Toast'

export default function TaskDetailPage() {
  const { id } = useParams<{ id: string }>()
  const taskId = parseInt(id || '0')
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const [activeTab, setActiveTab] = useState<'messages' | 'events' | 'issue' | 'pr'>('messages')
  const [messageInput, setMessageInput] = useState('')
  const [liveEvents, setLiveEvents] = useState<LiveEvent[]>([])
  const [pendingMessages, setPendingMessages] = useState<SessionMessage[]>([])
  const [thinking, setThinking] = useState(false)
  const [showCompleteOptions, setShowCompleteOptions] = useState(false)
  const [completeAction, setCompleteAction] = useState<'close_issue' | 'merge_pr'>('close_issue')
  const [headerCollapsed, setHeaderCollapsed] = useState(true)
  const endRef = useRef<HTMLDivElement>(null)
  const scrollContainerRef = useRef<HTMLDivElement>(null)
  const isNearBottomRef = useRef(true)
  const [showScrollBtn, setShowScrollBtn] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  // Fire core + issue/pr/git in parallel so the page renders the moment the core arrives.
  // Heavy external calls (GitHub API, local git fetch) no longer block initial render.
  const { data: taskDetail, isLoading } = useQuery({
    queryKey: ['task', taskId], queryFn: () => tasksApi.get(taskId),
    enabled: taskId > 0,
  })
  const issueQuery = useQuery({
    queryKey: ['task', taskId, 'issue'], queryFn: () => tasksApi.getIssue(taskId),
    enabled: taskId > 0,
  })
  const prQuery = useQuery({
    queryKey: ['task', taskId, 'pull-request'], queryFn: () => tasksApi.getPullRequest(taskId),
    enabled: taskId > 0,
  })
  const gitQuery = useQuery({
    queryKey: ['task', taskId, 'git'], queryFn: () => tasksApi.getGit(taskId),
    enabled: taskId > 0,
  })

  const task = taskDetail?.task
  const workspacePath = taskDetail?.workspace_path
  const issue = issueQuery.data?.issue
  const pullRequest = prQuery.data?.pull_request
  const git = gitQuery.data?.git
  const agentProfile = taskDetail?.agent_profile
  const promptSnapshot = task?.edges?.prompt_snapshot

  const sendMutation = useMutation({
    mutationFn: (content: string) => tasksApi.sendMessage(taskId, content),
    onMutate: (content: string) => {
      // Optimistic: show user message immediately
      const optimistic: SessionMessage = {
        id: -Date.now(), role: 'user', content_type: 'text', content,
        sequence: Date.now(), created_at: new Date().toISOString(),
      }
      setPendingMessages((prev) => [...prev, optimistic])
    },
    onSuccess: () => {
      setMessageInput(''); setThinking(true)
      setPendingMessages([])
      queryClient.invalidateQueries({ queryKey: ['task', taskId] })
    },
    onError: () => { setPendingMessages([]) },
  })
  const uploadMutation = useMutation({
    mutationFn: (file: File) => tasksApi.uploadAttachment(taskId, file),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }),
  })
  const completeMutation = useMutation({
    mutationFn: () => tasksApi.complete(taskId, { close_issue: true, merge_pr: completeAction === 'merge_pr' }),
    onSuccess: (data) => { queryClient.invalidateQueries({ queryKey: ['task', taskId] }); setShowCompleteOptions(false); toast(data.actions.join(', '), 'success') },
  })
  const pauseMutation = useMutation({ mutationFn: () => tasksApi.pause(taskId), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }) })
  const resumeMutation = useMutation({ mutationFn: () => tasksApi.resume(taskId), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }) })
  const retryMutation = useMutation({ mutationFn: () => tasksApi.retry(taskId), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }) })
  const cancelMutation = useMutation({ mutationFn: () => tasksApi.cancel(taskId), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }) })

  useEffect(() => {
    if (!taskId) return
    const unsub = subscribeToTaskEvents(taskId, (event) => {
      const seq = Number((event.data as Record<string, unknown> | null)?.['_sequence'] ?? 0)
      if (event.type !== 'connected' && event.type !== 'task.status')
        setLiveEvents((prev) => [...prev, { ...event, time: new Date().toLocaleTimeString(), sequence: seq }])
      if (event.type === 'run.step' || event.type === 'tool.call' || event.type === 'message.delta') setThinking(true)
      if (event.type === 'run.status') {
        const s = (event.data as Record<string, unknown>)?.status
        if (s === 'started' || s === 'running') setThinking(true)
        if (s === 'awaiting_user_confirmation') setThinking(false)
      }
      if (event.type === 'turn.completed') setThinking(false)
      if (event.type === 'task.failed' || event.type === 'task.completed') setThinking(false)
      if (event.type === 'task.status' || event.type === 'task.completed' || event.type === 'task.failed' || event.type === 'message.created') {
        // Core task (messages, status) - always refresh.
        queryClient.invalidateQueries({ queryKey: ['task', taskId], exact: true })
        // PR/git state often changes when the task transitions; issue state rarely does,
        // but a cheap re-fetch keeps things consistent without blocking the UI.
        if (event.type !== 'message.created') {
          queryClient.invalidateQueries({ queryKey: ['task', taskId, 'pull-request'] })
          queryClient.invalidateQueries({ queryKey: ['task', taskId, 'git'] })
        }
      }
    })
    return unsub
  }, [taskId, queryClient])

  // Whenever the task leaves the running state, the agent isn't producing output — clear Thinking.
  useEffect(() => {
    if (task && task.status !== 'running') setThinking(false)
  }, [task?.status])

  useEffect(() => {
    if (isNearBottomRef.current) {
      endRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [liveEvents, thinking])

  if (isLoading) return <TaskSkeleton />
  if (!task) return <div className="text-gray-500">Task not found</div>

  const sessions = task.edges.sessions || []
  const agentSessions = [...sessions]
    .filter((s) => !!s.provider_session_key)
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
  const latestAgentSession = agentSessions[0]
  const resumeHint = (() => {
    if (!latestAgentSession) return null
    const key = latestAgentSession.provider_session_key
    switch (agentProfile?.provider) {
      case 'claude_code': return `claude --resume ${key}`
      case 'codex': return null
      default: return null
    }
  })()
  const copySessionKey = async (key: string) => {
    try { await navigator.clipboard.writeText(key); toast('Session ID copied', 'success') }
    catch { toast('Copy failed', 'error') }
  }
  const historyMessages: SessionMessage[] = sessions.flatMap((s) => s.edges?.messages || []).sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
  const historyEvents: SessionEvent[] = sessions.flatMap((s) => s.edges?.events || []).sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
  const historyMaxSeq = Math.max(
    0,
    ...historyMessages.map((m) => m.sequence),
    ...historyEvents.map((e) => e.sequence),
  )
  // Drop live events that have already been persisted & polled back via history to avoid duplicates.
  const unseenLiveEvents = liveEvents.filter((e) => e.sequence === 0 || e.sequence > historyMaxSeq)
  const taskActive = isActiveStatus(task.status)
  // Agent is actively producing output only while running. waiting_user means the agent has
  // parked — any still-unresolved tool calls should render as "no result" rather than spinning.
  const agentBusy = task.status === 'running'
  const canSend = taskActive && !thinking && !sendMutation.isPending

  return (
    <div className="flex flex-col h-full min-h-0">
      <div className="task-head shrink-0">
        <div className="task-head-row1">
          <button onClick={() => setHeaderCollapsed(!headerCollapsed)} className="btn btn-icon btn-ghost" title="Toggle details">
            <svg className={`transition-transform ${headerCollapsed ? '' : 'rotate-90'}`} width={12} height={12} fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" /></svg>
          </button>
          <div className="task-head-title">
            <span className="hash">#{task.id}</span>
            <span className="sep-t">›</span>
            <span className="issue">issue #{task.issue_number}</span>
          </div>
          <StatusBadge status={task.status} />
          <span className="mono text-dim hidden sm:inline" style={{ fontSize: 11 }}>{task.type} · <Link to={`/projects/${task.edges.project?.id}`} className="lnk">{task.edges.project?.name}</Link></span>
          <div className="task-head-actions ml-auto">
            {task.status === 'running' && <Btn variant="secondary" size="sm" onClick={() => pauseMutation.mutate()} disabled={pauseMutation.isPending}>{pauseMutation.isPending ? '...' : 'Pause'}</Btn>}
            {task.status === 'paused' && <Btn variant="secondary" size="sm" onClick={() => resumeMutation.mutate()} disabled={resumeMutation.isPending}>{resumeMutation.isPending ? '...' : 'Resume'}</Btn>}
            {task.status === 'failed' && <Btn variant="secondary" size="sm" onClick={() => retryMutation.mutate()} disabled={retryMutation.isPending}>{retryMutation.isPending ? '...' : 'Retry'}</Btn>}
            {(taskActive || cancelMutation.isPending) && <Btn variant="danger" size="sm" onClick={() => cancelMutation.mutate()} disabled={cancelMutation.isPending}>{cancelMutation.isPending ? '...' : 'Cancel'}</Btn>}
            {task.status === 'waiting_user' && (
              <Btn size="sm" onClick={() => setShowCompleteOptions((v) => !v)} className="bg-green-600 hover:bg-green-700">Complete</Btn>
            )}
          </div>
        </div>

        {!headerCollapsed && (
          <div className="mb-4 pl-7 space-y-2">
            <p className="text-sm text-gray-500 sm:hidden">
              Issue #{task.issue_number} &middot; {task.type} &middot; <Link to={`/projects/${task.edges.project?.id}`} className="text-blue-600 hover:underline">{task.edges.project?.name}</Link>
            </p>
            {task.pr_number && <p className="text-sm text-gray-500">PR #{task.pr_number}</p>}
            {agentProfile && (
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <Tag color="purple">{agentProfile.provider}</Tag>
                <Tag color="gray">{agentProfile.model || 'default'}</Tag>
                {agentProfile.supports_image && <span className="text-gray-400" title="Supports image">img</span>}
                {agentProfile.supports_resume && <span className="text-gray-400" title="Supports resume">resume</span>}
              </div>
            )}
            {!agentProfile && task.agent_profile_id != null && <p className="text-sm text-gray-500">Agent #{task.agent_profile_id}</p>}
            {latestAgentSession && (
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <span className="text-gray-500">Agent Session:</span>
                <button onClick={() => copySessionKey(latestAgentSession.provider_session_key)}
                  className="inline-flex items-center rounded-full bg-gray-100 px-2.5 py-1 font-mono text-gray-700 hover:bg-gray-200"
                  title="Click to copy">
                  {latestAgentSession.provider_session_key}
                </button>
                {resumeHint && (
                  <button onClick={() => copySessionKey(resumeHint)}
                    className="inline-flex items-center rounded-full bg-blue-50 px-2.5 py-1 font-mono text-blue-700 hover:bg-blue-100"
                    title="Click to copy resume command">
                    {resumeHint}
                  </button>
                )}
                {agentSessions.length > 1 && (
                  <details className="inline">
                    <summary className="cursor-pointer text-gray-500 hover:text-gray-700">history ({agentSessions.length - 1})</summary>
                    <div className="mt-1 flex flex-wrap gap-1.5">
                      {agentSessions.slice(1).map((s) => (
                        <button key={s.id} onClick={() => copySessionKey(s.provider_session_key)}
                          className="inline-flex items-center rounded-full bg-gray-50 px-2 py-0.5 font-mono text-gray-600 hover:bg-gray-100"
                          title={`session #${s.id} · ${new Date(s.created_at).toLocaleString()}`}>
                          {s.provider_session_key}
                        </button>
                      ))}
                    </div>
                  </details>
                )}
              </div>
            )}
            {promptSnapshot && (
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <span className="text-gray-500">Prompt:</span>
                <span className="inline-flex items-center rounded-full bg-amber-50 px-2.5 py-1 text-amber-700 font-mono">{promptSnapshot.model_name}{promptSnapshot.model_version ? ` (${promptSnapshot.model_version})` : ''}</span>
                {promptSnapshot.system_prompt && <span className="text-gray-400">system: {promptSnapshot.system_prompt.length} chars</span>}
                {promptSnapshot.task_prompt && <span className="text-gray-400">task: {promptSnapshot.task_prompt.length} chars</span>}
              </div>
            )}
            {workspacePath && <p className="text-xs text-gray-400 font-mono">{workspacePath}</p>}
            {git?.branch && (
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <a href={`${task.edges.project?.repo_url}/tree/${encodeURIComponent(git.branch)}`} target="_blank" rel="noopener noreferrer"
                  className="inline-flex items-center rounded-full bg-blue-50 px-2.5 py-1 font-mono text-blue-700 hover:bg-blue-100">{git.branch}</a>
                {git.latest_commit && (
                  <>
                    <a href={`${task.edges.project?.repo_url}/commit/${git.latest_commit.hash}`} target="_blank" rel="noopener noreferrer"
                      className="inline-flex items-center rounded-full bg-gray-100 px-2.5 py-1 font-mono text-gray-700 hover:bg-gray-200">{git.latest_commit.hash}</a>
                    <span className="text-gray-500 truncate max-w-xs">{git.latest_commit.message}</span>
                  </>
                )}
                <button onClick={() => queryClient.invalidateQueries({ queryKey: ['task', taskId, 'git'] })}
                  className="inline-flex items-center rounded-full bg-gray-100 px-2 py-1 text-gray-600 hover:bg-gray-200" title="Refresh">↻</button>
              </div>
            )}
            {git?.branches && git.branches.length > 0 && (
              <details className="text-xs">
                <summary className="cursor-pointer text-gray-500 hover:text-gray-700">Branches ({git.branches.length})</summary>
                <div className="mt-1 flex flex-wrap gap-1.5">
                  {git.branches.map((b) => (
                    <a key={b.name} href={`${task.edges.project?.repo_url}/tree/${encodeURIComponent(b.name)}`} target="_blank" rel="noopener noreferrer"
                      className={`inline-flex items-center rounded-full px-2 py-0.5 font-mono ${b.name === git.branch ? 'bg-blue-100 text-blue-700' : 'bg-gray-50 text-gray-600 hover:bg-gray-100'}`}>{b.name}</a>
                  ))}
                </div>
              </details>
            )}
          </div>
        )}

        {showCompleteOptions && (
          <Card className="!mb-4">
            <CardContent className="!py-4">
              <p className="text-sm font-medium mb-3">Completion Actions</p>
              <div className="space-y-2 mb-4">
                <label className="flex items-center gap-3 cursor-pointer group">
                  <input type="radio" name="complete-action" checked={completeAction === 'close_issue'} onChange={() => setCompleteAction('close_issue')}
                    className="h-4 w-4 border-gray-300 text-blue-600 focus:ring-blue-500/20" />
                  <span className="text-sm font-medium text-gray-700 group-hover:text-gray-900">只关闭 Issue</span>
                </label>
                <label className="flex items-center gap-3 cursor-pointer group">
                  <input type="radio" name="complete-action" checked={completeAction === 'merge_pr'} onChange={() => setCompleteAction('merge_pr')}
                    className="h-4 w-4 border-gray-300 text-blue-600 focus:ring-blue-500/20" />
                  <span className="text-sm font-medium text-gray-700 group-hover:text-gray-900">Merge PR 并关闭 Issue</span>
                </label>
              </div>
              <div className="flex gap-2">
                <Btn onClick={() => completeMutation.mutate()} disabled={completeMutation.isPending}
                  className="bg-green-600 hover:bg-green-700">{completeMutation.isPending ? 'Completing...' : 'Confirm'}</Btn>
                <Btn variant="secondary" onClick={() => setShowCompleteOptions(false)}>Cancel</Btn>
              </div>
            </CardContent>
          </Card>
        )}

        {/* Tabs */}
        <div className="border-b border-gray-200">
          <div className="flex flex-wrap gap-4">
            {(['messages', 'events', 'issue', 'pr'] as const).map((tab) => {
              const counts: Record<string, string> = {
                messages: `${historyMessages.length}`,
                events: `${historyEvents.length + liveEvents.length}`,
                issue: '', pr: '',
              }
              return (
                <button key={tab} onClick={() => { setActiveTab(tab); if (tab === 'issue') queryClient.invalidateQueries({ queryKey: ['task', taskId, 'issue'] }); if (tab === 'pr') queryClient.invalidateQueries({ queryKey: ['task', taskId, 'pull-request'] }) }}
                  className={`pb-2 text-sm font-medium capitalize ${activeTab === tab ? 'border-b-2 border-blue-600 text-blue-600' : 'text-gray-500'}`}>
                  {tab}{counts[tab] ? ` (${counts[tab]})` : ''}
                </button>
              )
            })}
          </div>
        </div>
      </div>

      {/* Content */}
      <div className="rounded-xl border border-gray-200 bg-white shadow-sm overflow-hidden flex-1 min-h-0 flex flex-col mt-4">
        {activeTab === 'messages' ? (
          <MessagesTab historyMessages={historyMessages} liveEvents={unseenLiveEvents} thinking={thinking}
            taskActive={taskActive} agentBusy={agentBusy} canSend={canSend} messageInput={messageInput} setMessageInput={setMessageInput}
            sendMutation={sendMutation} uploadMutation={uploadMutation} fileInputRef={fileInputRef} endRef={endRef}
            scrollContainerRef={scrollContainerRef} isNearBottomRef={isNearBottomRef}
            showScrollBtn={showScrollBtn} setShowScrollBtn={setShowScrollBtn}
            promptSnapshot={promptSnapshot} pendingMessages={pendingMessages} />
        ) : activeTab === 'events' ? (
          <EventsTab historyEvents={historyEvents} liveEvents={unseenLiveEvents} endRef={endRef}
            scrollContainerRef={scrollContainerRef} isNearBottomRef={isNearBottomRef}
            showScrollBtn={showScrollBtn} setShowScrollBtn={setShowScrollBtn} />
        ) : activeTab === 'issue' ? (
          <IssueTab issue={issue ?? undefined} repoUrl={task.edges.project?.repo_url} loading={issueQuery.isLoading} />
        ) : (
          <PRTab pullRequest={pullRequest ?? undefined} loading={prQuery.isLoading} />
        )}
      </div>
    </div>
  )
}

// ============================================================
// Grouping: message ←→ tool_section ←→ message
// ============================================================

type LiveEvent = { type: string; data: unknown; time: string; sequence: number }

/** A single tool.call, optionally paired with one tool.result */
type ToolInvocation = {
  text: string
  tools: Array<{ name: string; input: Record<string, unknown> }>
  result?: unknown
  hasResult: boolean
  status: 'running' | 'completed' | 'failed' | 'no_result'  // derived from result data or absence
}

type GroupedItem =
  | { kind: 'user'; msg: SessionMessage }
  | { kind: 'message'; msg: SessionMessage }
  | { kind: 'tool_section'; invocations: ToolInvocation[]; loading: boolean }

type GroupedLiveItem =
  | { kind: 'message'; event: LiveEvent }
  | { kind: 'tool_section'; invocations: ToolInvocation[]; loading: boolean }
  | { kind: 'status'; event: LiveEvent }

function deriveStatus(result: unknown, hasResult: boolean): ToolInvocation['status'] {
  if (!hasResult) return 'no_result'
  const parsed = parseToolResult(result)
  if (parsed.type === 'command') {
    if (parsed.status === 'failed' || (parsed.exitCode != null && parsed.exitCode !== 0)) return 'failed'
    return 'completed'
  }
  return 'completed'
}

function buildInvocationFromMsg(callMsg: SessionMessage, resultMsg?: SessionMessage): ToolInvocation {
  const { tools, text } = normalizeToolCallData(callMsg.content)
  const result = resultMsg ? normalizeToolResultData(resultMsg.content) : undefined
  const hasResult = !!resultMsg
  return { text: text || toolsSummary(tools), tools, result, hasResult, status: deriveStatus(result, hasResult) }
}

function buildInvocationFromEvent(callEvent: LiveEvent, resultEvent?: LiveEvent): ToolInvocation {
  const { tools, text } = normalizeToolCallEventData(callEvent.data as Record<string, unknown>)
  const result = resultEvent ? normalizeToolResultEventData(resultEvent.data as Record<string, unknown>) : undefined
  const hasResult = !!resultEvent
  return { text: text || toolsSummary(tools), tools, result, hasResult, status: deriveStatus(result, hasResult) }
}

function toolsSummary(tools: Array<{ name: string; input: Record<string, unknown> }>): string {
  if (tools.length === 0) return 'Tool call'
  return tools.map((t) => {
    if (t.name === 'Bash') return `$ ${(t.input.command as string || '').slice(0, 80)}`
    if (t.name === 'Read') return `Read ${t.input.file_path}`
    if (t.name === 'Write') return `Write ${t.input.file_path}`
    if (t.name === 'Edit') return `Edit ${t.input.file_path}`
    return t.name
  }).join(', ')
}

function isToolResult(msg: SessionMessage): boolean {
  return msg.content_type === 'tool_result' || (msg.role === 'tool' && msg.content_type !== 'result')
}

function groupHistoryMessages(messages: SessionMessage[]): GroupedItem[] {
  const items: GroupedItem[] = []
  let pendingInvocations: ToolInvocation[] = []

  const flushTools = () => {
    if (pendingInvocations.length > 0) {
      items.push({ kind: 'tool_section', invocations: pendingInvocations, loading: false })
      pendingInvocations = []
    }
  }

  let i = 0
  while (i < messages.length) {
    const msg = messages[i]
    if (msg.role === 'user') {
      flushTools()
      items.push({ kind: 'user', msg })
      i++
    } else if (msg.content_type === 'tool_call') {
      // Collect consecutive tool_calls
      const calls: SessionMessage[] = []
      while (i < messages.length && messages[i].content_type === 'tool_call') {
        calls.push(messages[i])
        i++
      }
      // Collect consecutive tool_results
      const results: SessionMessage[] = []
      while (i < messages.length && isToolResult(messages[i])) {
        results.push(messages[i])
        i++
      }
      // Pair calls with results positionally
      for (let j = 0; j < calls.length; j++) {
        pendingInvocations.push(buildInvocationFromMsg(calls[j], results[j]))
      }
    } else if (isToolResult(msg)) {
      // orphaned result — skip
      i++
    } else {
      // result or text content_type — flush tools, add as message
      flushTools()
      items.push({ kind: 'message', msg })
      i++
    }
  }
  flushTools()
  return items
}

function groupLiveEvents(events: LiveEvent[]): GroupedLiveItem[] {
  const items: GroupedLiveItem[] = []
  let pendingInvocations: ToolInvocation[] = []
  let hasLoadingCall = false

  const flushTools = () => {
    if (pendingInvocations.length > 0) {
      items.push({ kind: 'tool_section', invocations: pendingInvocations, loading: hasLoadingCall })
      pendingInvocations = []
      hasLoadingCall = false
    }
  }

  let i = 0
  while (i < events.length) {
    const e = events[i]
    if (e.type === 'tool.call') {
      // Collect consecutive tool.calls
      const calls: LiveEvent[] = []
      while (i < events.length && events[i].type === 'tool.call') {
        calls.push(events[i])
        i++
      }
      // Collect consecutive tool.results
      const results: LiveEvent[] = []
      while (i < events.length && events[i].type === 'tool.result') {
        results.push(events[i])
        i++
      }
      // Pair calls with results positionally
      for (let j = 0; j < calls.length; j++) {
        const resultEvent = results[j]
        pendingInvocations.push(buildInvocationFromEvent(calls[j], resultEvent))
        if (!resultEvent) hasLoadingCall = true
      }
    } else if (e.type === 'tool.result') {
      i++ // orphan
    } else if (e.type === 'message.delta' || e.type === 'message.completed') {
      flushTools()
      items.push({ kind: 'message', event: e })
      i++
    } else if (e.type === 'task.completed' || e.type === 'task.failed' || (e.type === 'run.status' && (e.data as Record<string, unknown>)?.status === 'awaiting_user_confirmation')) {
      flushTools()
      items.push({ kind: 'status', event: e })
      i++
    } else {
      i++ // skip run.step, connected, etc.
    }
  }
  flushTools()
  return items
}

// ============================================================
// Messages Tab
// ============================================================

function handleScroll(e: React.UIEvent<HTMLDivElement>, isNearBottomRef: React.MutableRefObject<boolean>, setShowBtn?: (v: boolean) => void) {
  const el = e.currentTarget
  const threshold = 80
  const near = el.scrollHeight - el.scrollTop - el.clientHeight < threshold
  isNearBottomRef.current = near
  setShowBtn?.(!near)
}

function MessagesTab({
  historyMessages, liveEvents, thinking, taskActive, agentBusy, canSend,
  messageInput, setMessageInput, sendMutation, uploadMutation, fileInputRef, endRef,
  scrollContainerRef, isNearBottomRef, showScrollBtn, setShowScrollBtn,
  promptSnapshot, pendingMessages,
}: {
  historyMessages: SessionMessage[]; liveEvents: LiveEvent[]; thinking: boolean
  taskActive: boolean; agentBusy: boolean; canSend: boolean; messageInput: string; setMessageInput: (v: string) => void
  sendMutation: { mutate: (v: string) => void; isPending: boolean }; uploadMutation: { mutate: (f: File) => void }
  fileInputRef: React.RefObject<HTMLInputElement | null>; endRef: React.RefObject<HTMLDivElement | null>
  scrollContainerRef: React.RefObject<HTMLDivElement | null>; isNearBottomRef: React.MutableRefObject<boolean>
  showScrollBtn: boolean; setShowScrollBtn: (v: boolean) => void
  promptSnapshot?: PromptSnapshot | null; pendingMessages: SessionMessage[]
}) {
  const grouped = useMemo(() => groupHistoryMessages(historyMessages), [historyMessages])
  const groupedLive = useMemo(() => groupLiveEvents(liveEvents), [liveEvents])

  const scrollToBottom = () => {
    endRef.current?.scrollIntoView({ behavior: 'smooth' })
    isNearBottomRef.current = true
    setShowScrollBtn(false)
  }

  return (
    <div className="flex flex-col h-full relative">
      <div ref={scrollContainerRef} onScroll={(e) => handleScroll(e, isNearBottomRef, setShowScrollBtn)} className="flex-1 overflow-y-auto p-4 space-y-3">
        {!promptSnapshot && grouped.length === 0 && groupedLive.length === 0 && (
          <p className="text-gray-400 text-sm text-center py-8">No messages yet</p>
        )}

        {promptSnapshot?.system_prompt && <PromptBubble label="System Prompt" content={promptSnapshot.system_prompt} />}
        {promptSnapshot?.task_prompt && <PromptBubble label="Task Prompt" content={promptSnapshot.task_prompt} />}

        {grouped.map((item, i) => {
          if (item.kind === 'user') return <UserBubble key={`h-${i}`} msg={item.msg} />
          if (item.kind === 'message') return <MessageBubble key={`h-${i}`} msg={item.msg} />
          return <ToolSectionView key={`h-${i}`} invocations={item.invocations} loading={item.loading} defaultCollapsed={true} taskActive={agentBusy} />
        })}

        {groupedLive.map((item, i) => {
          if (item.kind === 'message') return <LiveMessageBubble key={`l-${i}`} event={item.event} />
          if (item.kind === 'status') return <LiveStatusBubble key={`l-${i}`} event={item.event} />
          return <ToolSectionView key={`l-${i}`} invocations={item.invocations} loading={item.loading && agentBusy} defaultCollapsed={true} taskActive={agentBusy} />
        })}

        {pendingMessages.map((msg) => <UserBubble key={`pending-${msg.id}`} msg={msg} />)}

        {thinking && <ThinkingIndicator />}
        <div ref={endRef} />
      </div>

      {showScrollBtn && <ScrollToBottomBtn onClick={scrollToBottom} />}

      {taskActive && (
        <div className="composer">
          <div className="composer-box">
            <div className="composer-prompt">
              <span className="prompt">❯</span>
              <textarea value={messageInput} onChange={(e) => setMessageInput(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); canSend && messageInput.trim() && sendMutation.mutate(messageInput) } }}
                placeholder={thinking ? 'Agent is working — waiting...' : 'Send a message or /command...'}
                disabled={thinking} rows={1} />
            </div>
            <div className="composer-bar">
              <input ref={fileInputRef} type="file" accept="image/*" style={{ display: 'none' }}
                onChange={(e) => { const f = e.target.files?.[0]; if (f) uploadMutation.mutate(f); e.target.value = '' }} />
              <button className="btn btn-icon btn-ghost" onClick={() => fileInputRef.current?.click()} disabled={thinking} title="Attach image">
                <svg width={13} height={13} fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round" viewBox="0 0 20 20"><path d="M14 8l-5 5a2 2 0 0 1-3-3l6-6a3 3 0 0 1 4 4l-7 7a4 4 0 0 1-6-6l5-5"/></svg>
              </button>
              <span className="hint"><kbd className="kbd">⏎</kbd> send · <kbd className="kbd">⇧⏎</kbd> newline</span>
              <Btn variant="accent" size="sm" onClick={() => messageInput.trim() && sendMutation.mutate(messageInput)}
                disabled={!canSend || !messageInput.trim()}>
                Send
              </Btn>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

// ============================================================
// Tool Section — collapsible group of tool invocations
// ============================================================

function ToolSectionView({ invocations, loading, defaultCollapsed, taskActive }: {
  invocations: ToolInvocation[]; loading: boolean; defaultCollapsed: boolean; taskActive: boolean
}) {
  // While running, auto-expand so the user sees live tool activity; once finished, auto-collapse.
  // User manual toggles win until the loading state transitions again.
  const [collapsed, setCollapsed] = useState(loading ? false : defaultCollapsed)
  const prevLoading = useRef(loading)
  useEffect(() => {
    if (prevLoading.current !== loading) {
      setCollapsed(!loading ? true : false)
      prevLoading.current = loading
    }
  }, [loading])
  const [expandAll, setExpandAll] = useState(false)

  if (invocations.length === 0 && !loading) return null

  const failedCount = invocations.filter((inv) => inv.status === 'failed').length
  const completedCount = invocations.filter((inv) => inv.status === 'completed').length
  const runningCount = invocations.filter((inv) => inv.status === 'no_result').length
  const isStillLoading = loading && taskActive

  return (
    <div className={`rounded-lg border overflow-hidden ${failedCount > 0 ? 'border-red-200 bg-red-50/30' : 'border-gray-200 bg-gray-50/50'}`}>
      <div className="flex items-center">
        <button
          onClick={() => setCollapsed(!collapsed)}
          className="flex-1 flex items-center gap-2 px-3 py-1.5 text-xs text-gray-500 hover:bg-gray-100 transition-colors"
        >
          <svg className={`w-3 h-3 shrink-0 transition-transform ${collapsed ? '' : 'rotate-90'}`} fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
          </svg>
          <span className="font-medium">{invocations.length} tool call{invocations.length !== 1 ? 's' : ''}</span>
          {completedCount > 0 && <span className="inline-flex items-center gap-1 text-green-600"><span className="w-1.5 h-1.5 rounded-full bg-green-400" />{completedCount}</span>}
          {failedCount > 0 && <span className="inline-flex items-center gap-1 text-red-600"><span className="w-1.5 h-1.5 rounded-full bg-red-400" />{failedCount}</span>}
          {runningCount > 0 && taskActive && <span className="inline-flex items-center gap-1 text-blue-500"><span className="w-1.5 h-1.5 rounded-full bg-blue-400" />{runningCount}</span>}
          {runningCount > 0 && !taskActive && <span className="inline-flex items-center gap-1 text-gray-400"><span className="w-1.5 h-1.5 rounded-full bg-gray-400" />{runningCount}</span>}
          {isStillLoading && (
            <div className="w-3 h-3 border-2 border-gray-300 border-t-blue-500 rounded-full animate-spin shrink-0" />
          )}
        </button>
        {!collapsed && (
          <button
            onClick={(e) => { e.stopPropagation(); setExpandAll(!expandAll) }}
            className="shrink-0 p-1 mr-1 text-gray-400 hover:text-gray-600 hover:bg-gray-100 rounded transition-colors"
            title={expandAll ? 'Collapse all' : 'Expand all'}
          >
            {expandAll ? (
              <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 14h16m-8-4V4m0 0L8 8m4-4l4 4M4 10h16m-8 4v6m0 0l-4-4m4 4l4-4" /></svg>
            ) : (
              <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 8h16M4 16h16m-8-4v6m0 0l-4-4m4 4l4-4M12 12V6m0 0L8 10m4-4l4 4" /></svg>
            )}
          </button>
        )}
      </div>

      {!collapsed && (
        <div className="border-t border-gray-200 divide-y divide-gray-100">
          {invocations.map((inv, i) => (
            <ToolInvocationRow key={i} inv={inv} taskActive={taskActive} forceExpanded={expandAll} />
          ))}
        </div>
      )}
    </div>
  )
}

const statusConfig = {
  completed: { dot: 'bg-green-400', text: 'text-green-600', label: '' },
  failed: { dot: 'bg-red-400', text: 'text-red-600', label: 'failed' },
  running: { dot: 'bg-blue-400', text: 'text-blue-500', label: '' },
  no_result: { dot: 'bg-gray-400', text: 'text-gray-400', label: '' },
}

function ToolInvocationRow({ inv, taskActive, forceExpanded }: { inv: ToolInvocation; taskActive: boolean; forceExpanded?: boolean }) {
  const [localOverride, setLocalOverride] = useState<boolean | null>(null)
  const isRunning = inv.status === 'no_result' && taskActive
  // Reset local override when forceExpanded or running-state changes so running→completed auto-collapses.
  const prevForce = useRef(forceExpanded)
  const prevRunning = useRef(isRunning)
  if (prevForce.current !== forceExpanded || prevRunning.current !== isRunning) {
    prevForce.current = forceExpanded
    prevRunning.current = isRunning
    setLocalOverride(null)
  }
  const expanded = localOverride !== null ? localOverride : (forceExpanded || isRunning)
  const cfg = statusConfig[isRunning ? 'running' : inv.status]

  return (
    <div>
      <button
        onClick={() => setLocalOverride(!expanded)}
        className={`w-full flex items-center gap-2 px-3 py-1.5 text-left text-xs hover:bg-gray-50 transition-colors ${inv.status === 'failed' ? 'bg-red-50/50' : ''}`}
      >
        {/* Status dot */}
        <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${cfg.dot}`} />
        <svg className={`w-3 h-3 shrink-0 text-gray-400 transition-transform ${expanded ? 'rotate-90' : ''}`} fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
        <span className={`truncate ${inv.status === 'failed' ? 'text-red-600' : 'text-gray-600'}`}>{inv.text}</span>
        {cfg.label && <span className={`shrink-0 font-mono uppercase text-[10px] ${cfg.text}`}>{cfg.label}</span>}
        {isRunning && (
          <div className="w-3 h-3 border-2 border-gray-300 border-t-blue-500 rounded-full animate-spin shrink-0 ml-auto" />
        )}
      </button>

      {expanded && (
        <div className="px-3 pb-2 pl-8">
          {inv.hasResult && inv.result != null ? (
            <ToolResultView result={inv.result} defaultExpanded={!!forceExpanded} />
          ) : (
            <div className="space-y-1">
              <ToolCallView tools={inv.tools} />
              {isRunning && (
                <div className="flex items-center gap-2 px-3 py-1.5 text-xs text-gray-400">
                  <div className="w-3 h-3 border-2 border-gray-300 border-t-blue-500 rounded-full animate-spin" />
                  <span>Running...</span>
                </div>
              )}
              {inv.status === 'no_result' && !taskActive && (
                <div className="px-3 py-1.5 text-xs text-gray-400">No result (task ended)</div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ============================================================
// Message bubbles
// ============================================================

function PromptBubble({ label, content }: { label: string; content: string }) {
  const [expanded, setExpanded] = useState(false)
  const preview = content.length > 200 ? content.slice(0, 200) + '...' : content

  return (
    <div className="flex justify-end">
      <div className="max-w-full sm:max-w-[85%] rounded-lg text-sm border border-gray-300 bg-gray-50 overflow-hidden">
        <button
          onClick={() => setExpanded(!expanded)}
          className="w-full flex items-center gap-2 px-3 py-1.5 text-left hover:bg-gray-100 transition-colors"
        >
          <span className="px-1.5 py-0.5 bg-gray-200 text-gray-600 rounded text-[10px] font-bold font-mono uppercase shrink-0">{label}</span>
          <span className="text-xs text-gray-400 truncate">{preview}</span>
          <svg className={`w-3 h-3 shrink-0 text-gray-400 transition-transform ml-auto ${expanded ? 'rotate-90' : ''}`} fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
          </svg>
        </button>
        {expanded && (
          <div className="border-t border-gray-200 px-4 py-3 max-h-80 overflow-y-auto">
            <Markdown className="text-xs text-gray-700 leading-relaxed">{content}</Markdown>
          </div>
        )}
      </div>
    </div>
  )
}

function UserBubble({ msg }: { msg: SessionMessage }) {
  const time = new Date(msg.created_at).toLocaleTimeString()
  return (
    <div className="bubble user">
      <div className="bubble-meta"><span className="role">you</span><span>· {time}</span></div>
      <div className="bubble-box">{msg.content}</div>
    </div>
  )
}

function MessageBubble({ msg }: { msg: SessionMessage }) {
  const time = new Date(msg.created_at).toLocaleTimeString()
  const isResult = msg.content_type === 'result'
  return (
    <div className={`bubble assistant${isResult ? ' result' : ''}`}>
      <div className="bubble-meta"><span className="role">assistant</span><span>· {time}</span></div>
      <div className="bubble-box"><Markdown>{msg.content}</Markdown></div>
    </div>
  )
}

function LiveMessageBubble({ event }: { event: LiveEvent }) {
  const data = event.data as Record<string, unknown>
  const content = data.content as string
  if (!content) return null

  const isFinal = event.type === 'message.completed'
  return (
    <div className={`bubble assistant${isFinal ? ' result' : ''}`}>
      <div className="bubble-meta"><span className="role">assistant</span><span>· {event.time}</span></div>
      <div className="bubble-box"><Markdown>{content}</Markdown></div>
    </div>
  )
}

function LiveStatusBubble({ event }: { event: LiveEvent }) {
  const data = event.data as Record<string, unknown>
  if (event.type === 'task.completed')
    return <div className="text-center py-2"><span className="px-3 py-1 bg-green-100 text-green-700 rounded-full text-xs">Task completed</span></div>
  if (event.type === 'task.failed')
    return <div className="text-center py-2"><span className="px-3 py-1 bg-red-100 text-red-700 rounded-full text-xs">Task failed: {data.error as string}</span></div>
  if (event.type === 'run.status' && data.status === 'awaiting_user_confirmation')
    return <div className="text-center py-2"><span className="px-3 py-1 bg-yellow-100 text-yellow-700 rounded-full text-xs">Task finished, waiting for manual completion</span></div>
  return null
}

function ScrollToBottomBtn({ onClick }: { onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="absolute bottom-20 right-6 w-8 h-8 rounded-full bg-white border border-gray-300 shadow-md flex items-center justify-center text-gray-500 hover:bg-gray-50 hover:text-gray-700 transition-colors z-10"
      title="Scroll to bottom"
    >
      <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 14l-7 7m0 0l-7-7m7 7V3" />
      </svg>
    </button>
  )
}

function ThinkingIndicator() {
  return (
    <div className="thinking">
      <span className="d"/><span className="d"/><span className="d"/>
      <span>thinking...</span>
    </div>
  )
}

// ============================================================
// Events Tab
// ============================================================

function EventsTab({ historyEvents, liveEvents, endRef, scrollContainerRef, isNearBottomRef, showScrollBtn, setShowScrollBtn }: {
  historyEvents: SessionEvent[]; liveEvents: LiveEvent[]; endRef: React.RefObject<HTMLDivElement | null>
  scrollContainerRef: React.RefObject<HTMLDivElement | null>; isNearBottomRef: React.MutableRefObject<boolean>
  showScrollBtn: boolean; setShowScrollBtn: (v: boolean) => void
}) {
  return (
    <div className="flex flex-col h-full p-4 relative">
      <div ref={scrollContainerRef} onScroll={(e) => handleScroll(e, isNearBottomRef, setShowScrollBtn)} className="flex-1 overflow-y-auto space-y-1 font-mono text-xs">
        {historyEvents.length === 0 && liveEvents.length === 0 && (
          <p className="text-gray-400 text-sm text-center py-8 font-sans">No events yet</p>
        )}
        {historyEvents.map((event) => {
          let payload: Record<string, unknown> = {}
          try { payload = JSON.parse(event.payload_json) } catch {}
          return (
            <div key={event.id} className="flex gap-2 py-1 border-b border-gray-50">
              <span className="text-gray-400 shrink-0">{new Date(event.created_at).toLocaleTimeString()}</span>
              <span className={`shrink-0 ${getEventColor(event.event_type)}`}>[{event.event_type}]</span>
              <span className="text-gray-700 break-all">{formatEventPayload(event.event_type, payload)}</span>
            </div>
          )
        })}
        {liveEvents.map((event, i) => (
          <div key={`live-${i}`} className="flex gap-2 py-1 border-b border-gray-50">
            <span className="text-gray-400 shrink-0">{event.time}</span>
            <span className={`shrink-0 ${getEventColor(event.type)}`}>[{event.type}]</span>
            <span className="text-gray-700 break-all">{formatEventPayload(event.type, event.data as Record<string, unknown>)}</span>
          </div>
        ))}
        <div ref={endRef} />
      </div>
      {showScrollBtn && <ScrollToBottomBtn onClick={() => {
        endRef.current?.scrollIntoView({ behavior: 'smooth' })
        isNearBottomRef.current = true
        setShowScrollBtn(false)
      }} />}
    </div>
  )
}

// ============================================================
// Issue & PR Tabs
// ============================================================

function IssueTab({ issue, repoUrl, loading }: { issue?: RepoIssue; repoUrl?: string; loading?: boolean }) {
  if (loading && !issue) return (
    <div className="flex-1 min-h-0 p-6 space-y-3 overflow-y-auto">
      <Skeleton className="h-5 w-40" />
      <Skeleton className="h-6 w-3/4" />
      <Skeleton className="h-24 w-full" />
    </div>
  )
  if (!issue) return <div className="p-6 text-sm text-gray-400 text-center">Issue data unavailable.</div>
  const stateColor = issue.state === 'open' ? 'green' : issue.state === 'closed' ? 'red' : 'gray'
  return (
    <div className="flex-1 min-h-0 p-6 space-y-4 overflow-y-auto">
      <div className="flex flex-wrap items-center gap-2">
        <a href={`${repoUrl}/issues/${issue.number}`} target="_blank" rel="noopener noreferrer" className="text-blue-600 hover:underline font-medium text-sm">Issue #{issue.number}</a>
        <Tag color={stateColor}>{issue.state}</Tag>
        <Tag color="gray">@{issue.user}</Tag>
        <span className="text-xs text-gray-400">{formatTimestamp(issue.created_at)}</span>
      </div>
      <h2 className="text-lg font-semibold">
        <a href={`${repoUrl}/issues/${issue.number}`} target="_blank" rel="noopener noreferrer" className="hover:text-blue-600 hover:underline">{issue.title}</a>
      </h2>
      <div className="flex flex-wrap gap-1.5">
        {issue.labels?.map((label) => <Tag key={label}>{label}</Tag>)}
      </div>
      <Markdown>{issue.body || '*No issue body*'}</Markdown>
    </div>
  )
}

function PRTab({ pullRequest, loading }: { pullRequest?: RepoPR; loading?: boolean }) {
  if (loading && !pullRequest) return (
    <div className="flex-1 min-h-0 p-6 space-y-3 overflow-y-auto">
      <Skeleton className="h-5 w-40" />
      <Skeleton className="h-6 w-3/4" />
      <Skeleton className="h-24 w-full" />
    </div>
  )
  if (!pullRequest) return <div className="p-6 text-sm text-gray-400 text-center">No PR associated with this task.</div>
  const checkColor = pullRequest.check_status === 'success' ? 'green' : pullRequest.check_status === 'failure' ? 'red' : pullRequest.check_status === 'pending' ? 'yellow' : 'gray'
  return (
    <div className="flex-1 min-h-0 p-6 space-y-4 overflow-y-auto">
      <div className="flex flex-wrap items-center gap-2">
        <a href={pullRequest.html_url} target="_blank" rel="noopener noreferrer" className="text-blue-600 hover:underline font-medium text-sm">PR #{pullRequest.number}</a>
        <Tag color={pullRequest.state === 'open' ? 'green' : pullRequest.state === 'closed' ? 'red' : 'gray'}>{pullRequest.state}</Tag>
        {pullRequest.user && <Tag color="gray">@{pullRequest.user}</Tag>}
        <Tag color="gray">{pullRequest.head} &rarr; {pullRequest.base}</Tag>
      </div>
      <h2 className="text-lg font-semibold">
        <a href={pullRequest.html_url} target="_blank" rel="noopener noreferrer" className="hover:text-blue-600 hover:underline">{pullRequest.title}</a>
      </h2>

      {/* Check status */}
      {pullRequest.check_status && (
        <div className="rounded-lg border border-gray-200 p-3 space-y-2">
          <div className="flex items-center gap-2">
            {pullRequest.check_status === 'success' && <svg className="w-4 h-4 text-green-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" /></svg>}
            {pullRequest.check_status === 'failure' && <svg className="w-4 h-4 text-red-500" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" /></svg>}
            {pullRequest.check_status === 'pending' && <div className="w-4 h-4 border-2 border-yellow-300 border-t-yellow-500 rounded-full animate-spin" />}
            <span className={`text-sm font-medium ${checkColor === 'green' ? 'text-green-700' : checkColor === 'red' ? 'text-red-700' : 'text-yellow-700'}`}>
              {pullRequest.check_status === 'success' ? 'All checks have passed' : pullRequest.check_status === 'failure' ? 'Some checks failed' : pullRequest.check_status === 'pending' ? 'Checks in progress' : pullRequest.check_status}
            </span>
          </div>
          {pullRequest.check_details && pullRequest.check_details.length > 0 && (
            <div className="space-y-1 pl-6">
              {pullRequest.check_details.map((cr, i) => (
                <div key={i} className="flex items-center gap-2 text-xs">
                  <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${cr.conclusion === 'success' ? 'bg-green-400' : cr.conclusion === 'failure' ? 'bg-red-400' : cr.status === 'in_progress' ? 'bg-yellow-400' : 'bg-gray-300'}`} />
                  <span className="text-gray-700">{cr.name}</span>
                  <span className="text-gray-400">{cr.conclusion || cr.status}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      <Markdown>{pullRequest.body || '*No PR body*'}</Markdown>
    </div>
  )
}

// ============================================================
// Skeleton
// ============================================================

function Skeleton({ className = '' }: { className?: string }) {
  return <div className={`animate-pulse bg-gray-200 rounded ${className}`} />
}

function TaskSkeleton() {
  return (
    <div className="flex flex-col h-full min-h-0">
      <div className="shrink-0 mb-4">
        <div className="flex items-center gap-3 mb-3">
          <Skeleton className="h-6 w-28" />
          <Skeleton className="h-5 w-16 rounded-full" />
          <Skeleton className="h-4 w-40 hidden sm:block" />
        </div>
        <div className="flex gap-4 border-b border-gray-200 pb-2">
          {[1, 2, 3, 4].map((i) => <Skeleton key={i} className="h-4 w-16" />)}
        </div>
      </div>
      <div className="rounded-xl border border-gray-200 bg-white shadow-sm flex-1 min-h-0 flex flex-col p-4 space-y-3">
        <Skeleton className="h-16 w-3/4 self-end rounded-lg" />
        <Skeleton className="h-12 w-2/3 rounded-lg" />
        <Skeleton className="h-8 w-full rounded-lg" />
        <Skeleton className="h-12 w-3/4 rounded-lg" />
        <Skeleton className="h-20 w-2/3 rounded-lg" />
      </div>
    </div>
  )
}

// ============================================================
// Helpers
// ============================================================

function isActiveStatus(status: TaskStatus): boolean {
  return ['running', 'waiting_user', 'queued', 'paused'].includes(status)
}

function getEventColor(type: string): string {
  if (type === 'run.step') return 'text-purple-600'
  if (type.startsWith('message')) return 'text-blue-600'
  if (type.startsWith('tool')) return 'text-green-600'
  if (type.startsWith('error') || type.startsWith('task.failed')) return 'text-red-600'
  return 'text-yellow-600'
}

function formatEventPayload(eventType: string, payload: Record<string, unknown>): string {
  if (eventType === 'run.step') return `${payload.step}: ${payload.detail}`
  return JSON.stringify(payload)
}

function formatTimestamp(value?: string): string {
  if (!value) return 'unknown'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function normalizeToolCallData(content: string): { tools: Array<{ name: string; input: Record<string, unknown> }>; text?: string } {
  let data: Record<string, unknown> = {}
  try { data = JSON.parse(content) } catch {}
  return normalizeToolCallEventData(data)
}

function normalizeToolCallEventData(data: Record<string, unknown>): { tools: Array<{ name: string; input: Record<string, unknown> }>; text?: string } {
  const tools = (data.tools as Array<{ name: string; input: Record<string, unknown> }>) || []
  const text = (data.text as string) || ''
  if (tools.length > 0 || text) return { tools, text }

  const item = data.item as Record<string, unknown> | undefined
  const command = (item?.command as string) || (data.command as string)
  if (command) {
    return { text: '', tools: [{ name: 'Bash', input: { command } }] }
  }
  return { tools: [], text }
}

function normalizeToolResultData(content: string): unknown {
  let data: Record<string, unknown> = {}
  try { data = JSON.parse(content) } catch {}
  return normalizeToolResultEventData(data)
}

function normalizeToolResultEventData(data: Record<string, unknown>): unknown {
  if (data.result) return data.result
  const item = data.item as Record<string, unknown> | undefined
  if (item?.aggregated_output || item?.command) {
    return { command: item.command, output: item.aggregated_output, status: item.status, exit_code: item.exit_code }
  }
  if (data.output || data.command) return data
  return data
}
