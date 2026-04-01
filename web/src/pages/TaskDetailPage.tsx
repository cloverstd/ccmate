import { useParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect, useRef } from 'react'
import { tasksApi, subscribeToTaskEvents, type TaskStatus } from '../lib/api'
import StatusBadge from '../components/StatusBadge'

export default function TaskDetailPage() {
  const { id } = useParams<{ id: string }>()
  const taskId = parseInt(id || '0')
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState<'messages' | 'events'>('messages')
  const [messageInput, setMessageInput] = useState('')
  const [liveEvents, setLiveEvents] = useState<Array<{ type: string; data: unknown; time: string }>>([])
  const eventsEndRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const { data: task, isLoading } = useQuery({
    queryKey: ['task', taskId],
    queryFn: () => tasksApi.get(taskId),
    enabled: taskId > 0,
    refetchInterval: 5000,
  })

  const sendMutation = useMutation({
    mutationFn: (content: string) => tasksApi.sendMessage(taskId, content),
    onSuccess: () => { setMessageInput(''); queryClient.invalidateQueries({ queryKey: ['task', taskId] }) },
  })

  const uploadMutation = useMutation({
    mutationFn: (file: File) => tasksApi.uploadAttachment(taskId, file),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }),
  })

  const pauseMutation = useMutation({ mutationFn: () => tasksApi.pause(taskId), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }) })
  const resumeMutation = useMutation({ mutationFn: () => tasksApi.resume(taskId), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }) })
  const retryMutation = useMutation({ mutationFn: () => tasksApi.retry(taskId), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }) })
  const cancelMutation = useMutation({ mutationFn: () => tasksApi.cancel(taskId), onSuccess: () => queryClient.invalidateQueries({ queryKey: ['task', taskId] }) })

  useEffect(() => {
    if (!taskId || !task || !isActiveStatus(task.status)) return
    const unsubscribe = subscribeToTaskEvents(taskId, (event) => {
      setLiveEvents((prev) => [...prev, { ...event, time: new Date().toLocaleTimeString() }])
      if (event.type === 'task.completed' || event.type === 'task.failed') queryClient.invalidateQueries({ queryKey: ['task', taskId] })
    })
    return unsubscribe
  }, [taskId, task?.status])

  useEffect(() => { eventsEndRef.current?.scrollIntoView({ behavior: 'smooth' }) }, [liveEvents])

  if (isLoading) return <div className="text-gray-500">Loading...</div>
  if (!task) return <div className="text-gray-500">Task not found</div>

  const sessions = task.edges.sessions || []
  const messages = sessions.flatMap((s) => s.edges?.messages || []).sort((a, b) => a.sequence - b.sequence)

  return (
    <div>
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 mb-6">
        <div>
          <h1 className="text-2xl font-bold">Task #{task.id}</h1>
          <p className="text-sm text-gray-500 mt-1">
            Issue #{task.issue_number} &middot; {task.type} &middot; {task.edges.project?.name}
            {task.pr_number && <> &middot; PR #{task.pr_number}</>}
          </p>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          <StatusBadge status={task.status} />
          {task.status === 'running' && <button onClick={() => pauseMutation.mutate()} className="px-3 py-1 text-sm border rounded hover:bg-gray-50">Pause</button>}
          {task.status === 'paused' && <button onClick={() => resumeMutation.mutate()} className="px-3 py-1 text-sm border rounded hover:bg-gray-50">Resume</button>}
          {task.status === 'failed' && <button onClick={() => retryMutation.mutate()} className="px-3 py-1 text-sm border rounded hover:bg-gray-50">Retry</button>}
          {isActiveStatus(task.status) && <button onClick={() => cancelMutation.mutate()} className="px-3 py-1 text-sm border border-red-300 text-red-600 rounded hover:bg-red-50">Cancel</button>}
        </div>
      </div>

      <div className="border-b border-gray-200 mb-4">
        <div className="flex gap-4">
          <button onClick={() => setActiveTab('messages')} className={`pb-2 text-sm font-medium ${activeTab === 'messages' ? 'border-b-2 border-blue-600 text-blue-600' : 'text-gray-500'}`}>Messages</button>
          <button onClick={() => setActiveTab('events')} className={`pb-2 text-sm font-medium ${activeTab === 'events' ? 'border-b-2 border-blue-600 text-blue-600' : 'text-gray-500'}`}>Events ({liveEvents.length})</button>
        </div>
      </div>

      <div className="bg-white rounded-lg shadow">
        {activeTab === 'messages' ? (
          <div className="p-4">
            <div className="space-y-4 max-h-[60vh] overflow-y-auto">
              {messages.length === 0 && liveEvents.length === 0 && <p className="text-gray-400 text-sm text-center py-8">No messages yet</p>}
              {messages.map((msg) => (
                <div key={msg.id} className={`flex ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}>
                  <div className={`max-w-[80%] px-4 py-2 rounded-lg text-sm ${msg.role === 'user' ? 'bg-blue-600 text-white' : 'bg-gray-100 text-gray-800'}`}>
                    <pre className="whitespace-pre-wrap font-sans">{msg.content}</pre>
                    <div className={`text-xs mt-1 ${msg.role === 'user' ? 'text-blue-200' : 'text-gray-400'}`}>{new Date(msg.created_at).toLocaleTimeString()}</div>
                  </div>
                </div>
              ))}
              {liveEvents.filter((e) => e.type === 'message.delta' || e.type === 'message.completed').map((e, i) => (
                <div key={`live-${i}`} className="flex justify-start">
                  <div className="max-w-[80%] px-4 py-2 rounded-lg text-sm bg-gray-100 text-gray-800">
                    <pre className="whitespace-pre-wrap font-sans">{(e.data as Record<string, string>)?.content || ''}</pre>
                    <div className="text-xs mt-1 text-gray-400">{e.time}</div>
                  </div>
                </div>
              ))}
              <div ref={eventsEndRef} />
            </div>

            {isActiveStatus(task.status) && (
              <div className="mt-4 flex gap-2">
                <input value={messageInput} onChange={(e) => setMessageInput(e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && !e.shiftKey && messageInput.trim() && sendMutation.mutate(messageInput)}
                  placeholder="Send a message..." className="flex-1 px-3 py-2 border border-gray-300 rounded text-sm" />
                <input ref={fileInputRef} type="file" accept="image/*" className="hidden"
                  onChange={(e) => { const f = e.target.files?.[0]; if (f) uploadMutation.mutate(f); e.target.value = '' }} />
                <button onClick={() => fileInputRef.current?.click()} className="px-3 py-2 border border-gray-300 rounded text-sm hover:bg-gray-50" title="Upload image">
                  📎
                </button>
                <button onClick={() => messageInput.trim() && sendMutation.mutate(messageInput)}
                  disabled={sendMutation.isPending || !messageInput.trim()}
                  className="px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50">Send</button>
              </div>
            )}
          </div>
        ) : (
          <div className="p-4">
            <div className="space-y-1 max-h-[60vh] overflow-y-auto font-mono text-xs">
              {liveEvents.length === 0 && <p className="text-gray-400 text-sm text-center py-8 font-sans">No events yet</p>}
              {liveEvents.map((event, i) => (
                <div key={i} className="flex gap-2 py-1 border-b border-gray-50">
                  <span className="text-gray-400 shrink-0">{event.time}</span>
                  <span className={`shrink-0 ${getEventColor(event.type)}`}>[{event.type}]</span>
                  <span className="text-gray-700 break-all">{typeof event.data === 'string' ? event.data : JSON.stringify(event.data)}</span>
                </div>
              ))}
              <div ref={eventsEndRef} />
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function isActiveStatus(status: TaskStatus): boolean {
  return ['running', 'waiting_user', 'queued', 'paused'].includes(status)
}

function getEventColor(type: string): string {
  if (type.startsWith('message')) return 'text-blue-600'
  if (type.startsWith('tool')) return 'text-green-600'
  if (type.startsWith('error') || type.startsWith('task.failed')) return 'text-red-600'
  return 'text-yellow-600'
}
