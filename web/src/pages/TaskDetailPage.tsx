import { useParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect, useRef } from 'react'
import Markdown from 'react-markdown'
import { tasksApi, subscribeToTaskEvents, type TaskStatus, type SessionMessage, type SessionEvent } from '../lib/api'
import StatusBadge from '../components/StatusBadge'
import { ToolCallView, ToolResultView } from '../components/ToolView'

export default function TaskDetailPage() {
  const { id } = useParams<{ id: string }>()
  const taskId = parseInt(id || '0')
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState<'messages' | 'events'>('messages')
  const [messageInput, setMessageInput] = useState('')
  const [liveEvents, setLiveEvents] = useState<Array<{ type: string; data: unknown; time: string }>>([])
  const [thinking, setThinking] = useState(false)
  const endRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const { data: taskDetail, isLoading } = useQuery({
    queryKey: ['task', taskId], queryFn: () => tasksApi.get(taskId),
    enabled: taskId > 0, refetchInterval: 5000,
  })

  const task = taskDetail?.task
  const workspacePath = taskDetail?.workspace_path

  const sendMutation = useMutation({
    mutationFn: (content: string) => tasksApi.sendMessage(taskId, content),
    onSuccess: () => {
      setMessageInput('')
      setThinking(true)
      queryClient.invalidateQueries({ queryKey: ['task', taskId] })
    },
  })
  const uploadMutation = useMutation({
    mutationFn: (file: File) => tasksApi.uploadAttachment(taskId, file),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }),
  })
  const completeMutation = useMutation({
    mutationFn: () => tasksApi.complete(taskId),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['task', taskId] })
      alert(data.actions.join('\n'))
    },
  })
  const pauseMutation = useMutation({ mutationFn: () => tasksApi.pause(taskId), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }) })
  const resumeMutation = useMutation({ mutationFn: () => tasksApi.resume(taskId), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }) })
  const retryMutation = useMutation({ mutationFn: () => tasksApi.retry(taskId), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }) })
  const cancelMutation = useMutation({ mutationFn: () => tasksApi.cancel(taskId), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }) })

  useEffect(() => {
    if (!taskId || !task || !isActiveStatus(task.status)) return
    const unsub = subscribeToTaskEvents(taskId, (event) => {
      setLiveEvents((prev) => [...prev, { ...event, time: new Date().toLocaleTimeString() }])
      // message.completed or result means claude is done thinking
      if (event.type === 'message.completed' || event.type === 'task.completed' || event.type === 'task.failed') {
        setThinking(false)
      }
      // New activity means claude is thinking
      if (event.type === 'message.delta' || event.type === 'tool.call') {
        setThinking(true)
      }
      if (event.type === 'task.completed' || event.type === 'task.failed')
        queryClient.invalidateQueries({ queryKey: ['task', taskId] })
    })
    return unsub
  }, [taskId, task?.status])

  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }) }, [liveEvents, thinking])

  if (isLoading) return <div className="py-8 text-center text-gray-400 text-sm">Loading...</div>
  if (!task) return <div className="text-gray-500">Task not found</div>

  const sessions = task.edges.sessions || []
  const historyMessages: SessionMessage[] = sessions.flatMap((s) => s.edges?.messages || []).sort((a, b) => a.sequence - b.sequence)
  const historyEvents: SessionEvent[] = sessions.flatMap((s) => s.edges?.events || []).sort((a, b) => a.sequence - b.sequence)
  const canSend = isActiveStatus(task.status) && !thinking && !sendMutation.isPending

  return (
    <div>
      {/* Header */}
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 mb-6">
        <div>
          <h1 className="text-2xl font-bold">Task #{task.id}</h1>
          <p className="text-sm text-gray-500 mt-1">
            Issue #{task.issue_number} &middot; {task.type} &middot; {task.edges.project?.name}
            {task.pr_number && <> &middot; PR #{task.pr_number}</>}
          </p>
          {workspacePath && <p className="text-xs text-gray-400 mt-1 font-mono">{workspacePath}</p>}
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <StatusBadge status={task.status} />
          {task.status === 'running' && <button onClick={() => pauseMutation.mutate()} className="px-3 py-1 text-sm border rounded hover:bg-gray-50">Pause</button>}
          {task.status === 'paused' && <button onClick={() => resumeMutation.mutate()} className="px-3 py-1 text-sm border rounded hover:bg-gray-50">Resume</button>}
          {task.status === 'failed' && <button onClick={() => retryMutation.mutate()} className="px-3 py-1 text-sm border rounded hover:bg-gray-50">Retry</button>}
          {isActiveStatus(task.status) && <button onClick={() => cancelMutation.mutate()} className="px-3 py-1 text-sm border border-red-300 text-red-600 rounded hover:bg-red-50">Cancel</button>}
          {(task.status === 'succeeded' || task.status === 'running' || task.status === 'paused') && (
            <button onClick={() => { if (confirm('This will merge the PR (if exists) and close the issue. Continue?')) completeMutation.mutate() }}
              disabled={completeMutation.isPending}
              className="px-3 py-1 text-sm bg-green-600 text-white rounded hover:bg-green-700 disabled:opacity-50">
              {completeMutation.isPending ? 'Completing...' : 'Complete'}
            </button>
          )}
        </div>
      </div>

      {/* Tabs */}
      <div className="border-b border-gray-200 mb-4">
        <div className="flex gap-4">
          <button onClick={() => setActiveTab('messages')}
            className={`pb-2 text-sm font-medium ${activeTab === 'messages' ? 'border-b-2 border-blue-600 text-blue-600' : 'text-gray-500'}`}>
            Messages ({historyMessages.length})
          </button>
          <button onClick={() => setActiveTab('events')}
            className={`pb-2 text-sm font-medium ${activeTab === 'events' ? 'border-b-2 border-blue-600 text-blue-600' : 'text-gray-500'}`}>
            Events ({historyEvents.length + liveEvents.length})
          </button>
        </div>
      </div>

      <div className="bg-white rounded-lg shadow">
        {activeTab === 'messages' ? (
          <div className="p-4">
            <div className="space-y-3 max-h-[65vh] overflow-y-auto">
              {historyMessages.length === 0 && liveEvents.length === 0 && <p className="text-gray-400 text-sm text-center py-8">No messages yet</p>}
              {historyMessages.map((msg) => <MessageBubble key={msg.id} msg={msg} />)}
              {liveEvents.map((e, i) => <LiveEventBubble key={`live-${i}`} event={e} />)}
              {thinking && <ThinkingIndicator />}
              <div ref={endRef} />
            </div>

            {/* Input */}
            {isActiveStatus(task.status) && (
              <div className="mt-4">
                {thinking && <div className="text-xs text-gray-400 mb-2 flex items-center gap-1"><span className="animate-pulse">Claude is thinking...</span></div>}
                <div className="flex gap-2">
                  <input value={messageInput} onChange={(e) => setMessageInput(e.target.value)}
                    onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && canSend && messageInput.trim() && sendMutation.mutate(messageInput)}
                    placeholder={thinking ? 'Waiting for Claude to finish...' : 'Send a message...'}
                    disabled={thinking}
                    className="flex-1 px-3 py-2 border border-gray-300 rounded text-sm disabled:bg-gray-50 disabled:text-gray-400" />
                  <input ref={fileInputRef} type="file" accept="image/*" className="hidden"
                    onChange={(e) => { const f = e.target.files?.[0]; if (f) uploadMutation.mutate(f); e.target.value = '' }} />
                  <button onClick={() => fileInputRef.current?.click()} disabled={thinking}
                    className="px-3 py-2 border border-gray-300 rounded text-sm hover:bg-gray-50 disabled:opacity-50" title="Upload image">
                    📎
                  </button>
                  <button onClick={() => messageInput.trim() && sendMutation.mutate(messageInput)}
                    disabled={!canSend || !messageInput.trim()}
                    className="px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed">
                    Send
                  </button>
                </div>
              </div>
            )}
          </div>
        ) : (
          <div className="p-4">
            <div className="space-y-1 max-h-[65vh] overflow-y-auto font-mono text-xs">
              {historyEvents.length === 0 && liveEvents.length === 0 && <p className="text-gray-400 text-sm text-center py-8 font-sans">No events yet</p>}
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
          </div>
        )}
      </div>
    </div>
  )
}

// --- Thinking indicator ---

function ThinkingIndicator() {
  return (
    <div className="flex justify-start">
      <div className="px-4 py-3 rounded-lg bg-gray-50 border border-gray-200">
        <div className="flex items-center gap-2">
          <div className="flex gap-1">
            <div className="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style={{ animationDelay: '0ms' }} />
            <div className="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style={{ animationDelay: '150ms' }} />
            <div className="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style={{ animationDelay: '300ms' }} />
          </div>
          <span className="text-xs text-gray-500">Thinking...</span>
        </div>
      </div>
    </div>
  )
}

// --- Message rendering ---

function MessageBubble({ msg }: { msg: { id: number; role: string; content_type: string; content: string; created_at: string } }) {
  const time = new Date(msg.created_at).toLocaleTimeString()

  if (msg.role === 'user') {
    return (
      <div className="flex justify-end">
        <div className="max-w-[80%] px-4 py-2 rounded-lg text-sm bg-blue-600 text-white">
          <pre className="whitespace-pre-wrap font-sans">{msg.content}</pre>
          <div className="text-xs mt-1 text-blue-200">{time}</div>
        </div>
      </div>
    )
  }

  if (msg.content_type === 'tool_call') {
    let data: Record<string, unknown> = {}
    try { data = JSON.parse(msg.content) } catch {}
    const tools = (data.tools as Array<Record<string, unknown>>) || []
    const text = (data.text as string) || ''
    return (
      <div className="flex justify-start">
        <div className="max-w-[90%] space-y-2">
          {text && (
            <div className="px-4 py-2 rounded-lg text-sm bg-gray-100 text-gray-800 prose prose-sm max-w-none">
              <Markdown>{text}</Markdown>
            </div>
          )}
          <ToolCallView tools={tools as Array<{ name: string; input: Record<string, unknown> }>} />
          <div className="text-xs text-gray-400">{time}</div>
        </div>
      </div>
    )
  }

  if (msg.content_type === 'tool_result') {
    let data: Record<string, unknown> = {}
    try { data = JSON.parse(msg.content) } catch {}
    return (
      <div className="flex justify-start">
        <div className="max-w-[90%]">
          <ToolResultView result={data.result || data} />
          <div className="text-xs text-gray-400 mt-1">{time}</div>
        </div>
      </div>
    )
  }

  if (msg.content_type === 'result') {
    return (
      <div className="flex justify-start">
        <div className="max-w-[80%] px-4 py-2 rounded-lg text-sm bg-blue-50 border border-blue-200">
          <span className="px-1.5 py-0.5 bg-blue-200 text-blue-800 rounded text-xs font-bold font-mono mb-2 inline-block">COMPLETED</span>
          <div className="prose prose-sm max-w-none text-blue-900">
            <Markdown>{msg.content}</Markdown>
          </div>
          <div className="text-xs text-blue-400 mt-1">{time}</div>
        </div>
      </div>
    )
  }

  // Regular text (assistant message.delta)
  return (
    <div className="flex justify-start">
      <div className="max-w-[80%] px-4 py-2 rounded-lg text-sm bg-gray-100 text-gray-800">
        <div className="prose prose-sm max-w-none">
          <Markdown>{msg.content}</Markdown>
        </div>
        <div className="text-xs mt-1 text-gray-400">{time}</div>
      </div>
    </div>
  )
}

function LiveEventBubble({ event }: { event: { type: string; data: unknown; time: string } }) {
  const data = event.data as Record<string, unknown>
  if (!data) return null
  if (event.type === 'run.step' || event.type === 'run.status' || event.type === 'connected') return null

  if (event.type === 'message.delta' || event.type === 'message.completed') {
    const content = data.content as string
    if (!content) return null
    return (
      <div className="flex justify-start">
        <div className="max-w-[80%] px-4 py-2 rounded-lg text-sm bg-gray-100 text-gray-800">
          <div className="prose prose-sm max-w-none"><Markdown>{content}</Markdown></div>
          <div className="text-xs mt-1 text-gray-400">{event.time}</div>
        </div>
      </div>
    )
  }

  if (event.type === 'tool.call') {
    const tools = (data.tools as Array<{ name: string; input: Record<string, unknown> }>) || []
    const text = data.text as string || ''
    return (
      <div className="flex justify-start">
        <div className="max-w-[90%] space-y-2">
          {text && <div className="px-4 py-2 rounded-lg text-sm bg-gray-100 prose prose-sm max-w-none"><Markdown>{text}</Markdown></div>}
          <ToolCallView tools={tools} />
          <div className="text-xs text-gray-400">{event.time}</div>
        </div>
      </div>
    )
  }

  if (event.type === 'tool.result') {
    return (
      <div className="flex justify-start">
        <div className="max-w-[90%]"><ToolResultView result={data.result || data} /></div>
      </div>
    )
  }

  if (event.type === 'error') {
    return (
      <div className="flex justify-start">
        <div className="max-w-[90%] px-3 py-2 rounded border border-red-200 bg-red-50 text-sm text-red-700">
          <span className="font-mono text-xs font-bold">ERROR </span>{data.message as string}
        </div>
      </div>
    )
  }

  if (event.type === 'task.completed') {
    return <div className="text-center py-2"><span className="px-3 py-1 bg-green-100 text-green-700 rounded-full text-xs">Task completed</span></div>
  }
  if (event.type === 'task.failed') {
    return <div className="text-center py-2"><span className="px-3 py-1 bg-red-100 text-red-700 rounded-full text-xs">Task failed: {data.error as string}</span></div>
  }
  return null
}

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
