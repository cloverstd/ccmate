import { useParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { projectsApi, promptsApi } from '../lib/api'

export default function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>()
  const projectId = parseInt(id || '0')
  const queryClient = useQueryClient()

  const { data: project, isLoading } = useQuery({
    queryKey: ['project', projectId],
    queryFn: () => projectsApi.get(projectId),
    enabled: projectId > 0,
  })

  const { data: rules } = useQuery({
    queryKey: ['label-rules', projectId],
    queryFn: () => projectsApi.listLabelRules(projectId),
    enabled: projectId > 0,
  })

  const { data: templates } = useQuery({
    queryKey: ['prompt-templates'],
    queryFn: promptsApi.list,
  })

  const [newLabel, setNewLabel] = useState('')
  const [newTrigger, setNewTrigger] = useState('auto')
  const [newTemplate, setNewTemplate] = useState({ name: '', system_prompt: '', task_prompt: '' })
  const [showTemplateForm, setShowTemplateForm] = useState(false)

  const createRule = useMutation({
    mutationFn: () => projectsApi.createLabelRule(projectId, { issue_label: newLabel, trigger_mode: newTrigger }),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['label-rules'] }); setNewLabel('') },
  })

  const deleteRule = useMutation({
    mutationFn: (ruleId: number) => projectsApi.deleteLabelRule(ruleId),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['label-rules'] }),
  })

  const createTemplate = useMutation({
    mutationFn: () => promptsApi.create(newTemplate),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['prompt-templates'] }); setShowTemplateForm(false); setNewTemplate({ name: '', system_prompt: '', task_prompt: '' }) },
  })

  const deleteTemplate = useMutation({
    mutationFn: (tid: number) => promptsApi.delete(tid),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['prompt-templates'] }),
  })

  if (isLoading) return <div className="text-gray-500">Loading...</div>
  if (!project) return <div className="text-gray-500">Project not found</div>

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">{project.name}</h1>

      <div className="bg-white rounded-lg shadow p-6">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div><label className="block text-sm font-medium text-gray-500">Repository URL</label><p className="mt-1 text-sm">{project.repo_url}</p></div>
          <div><label className="block text-sm font-medium text-gray-500">Default Branch</label><p className="mt-1 text-sm">{project.default_branch}</p></div>
          <div><label className="block text-sm font-medium text-gray-500">Max Concurrency</label><p className="mt-1 text-sm">{project.max_concurrency}</p></div>
          <div><label className="block text-sm font-medium text-gray-500">Mode</label><p className="mt-1 text-sm">{project.auto_mode ? 'Automatic' : 'Manual'}</p></div>
        </div>
      </div>

      {/* Label Rules */}
      <div className="mt-6 bg-white rounded-lg shadow p-6">
        <h2 className="text-lg font-semibold mb-4">Label Rules</h2>
        {rules && rules.length > 0 && (
          <div className="space-y-2 mb-4">
            {rules.map((rule) => (
              <div key={rule.id} className="flex items-center justify-between py-2 px-3 bg-gray-50 rounded">
                <div className="flex items-center gap-3">
                  <span className="px-2 py-0.5 bg-blue-100 text-blue-700 rounded text-xs">{rule.issue_label}</span>
                  <span className="text-xs text-gray-500">{rule.trigger_mode}</span>
                </div>
                <button onClick={() => deleteRule.mutate(rule.id)} className="text-red-500 text-xs hover:underline">Delete</button>
              </div>
            ))}
          </div>
        )}
        <div className="flex gap-2">
          <input value={newLabel} onChange={(e) => setNewLabel(e.target.value)} placeholder="Label name" className="flex-1 px-3 py-1.5 border rounded text-sm" />
          <select value={newTrigger} onChange={(e) => setNewTrigger(e.target.value)} className="px-3 py-1.5 border rounded text-sm">
            <option value="auto">Auto</option>
            <option value="manual">Manual</option>
          </select>
          <button onClick={() => newLabel && createRule.mutate()} disabled={!newLabel} className="px-3 py-1.5 bg-blue-600 text-white rounded text-sm disabled:opacity-50">Add</button>
        </div>
      </div>

      {/* Prompt Templates */}
      <div className="mt-6 bg-white rounded-lg shadow p-6">
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
        {templates && templates.length > 0 ? (
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
    </div>
  )
}
