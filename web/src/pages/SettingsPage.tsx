import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { settingsApi, modelsApi, authApi, promptsApi, type AgentProfile, type PromptTemplate } from '../lib/api'
import PromptEditor from '../components/PromptEditor'
import { Card, CardHeader, CardContent, CardFooter, Label, Input, Select, Checkbox, Btn, Tag, Alert, Separator } from '../components/ui'
import { useToast } from '../components/Toast'

function parseJSONArray<T>(val: string | undefined): T[] {
  if (!val) return []
  try { return JSON.parse(val) } catch { return [] }
}

// ============================================================
// Settings Page
// ============================================================

export default function SettingsPage() {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const { data: settings, isLoading } = useQuery({ queryKey: ['settings'], queryFn: settingsApi.get })
  const { data: models } = useQuery({ queryKey: ['models'], queryFn: modelsApi.list })
  const { data: globalTemplates } = useQuery({ queryKey: ['global-templates'], queryFn: () => promptsApi.list({ scope: 'global' }) })
  const { data: me } = useQuery({ queryKey: ['me'], queryFn: authApi.me })
  const { data: ghPerms, refetch: recheckPerms, isFetching: permsTesting } = useQuery({
    queryKey: ['gh-perms'], queryFn: settingsApi.checkGitHubPermissions, enabled: true,
  })

  const [form, setForm] = useState<Record<string, string>>({})
  const [passkeyMsg, setPasskeyMsg] = useState('')
  const [allowedUsers, setAllowedUsers] = useState<string[]>([])
  const [newUser, setNewUser] = useState('')
  const [labelRules, setLabelRules] = useState<{ label: string; trigger_mode: string }[]>([])
  const [newLabel, setNewLabel] = useState('')
  const [newTrigger, setNewTrigger] = useState('auto')
  const [agentProviders, setAgentProviders] = useState<{ name: string; binary: string }[]>([])
  const [rpOrigins, setRpOrigins] = useState<string[]>([])
  const [newOrigin, setNewOrigin] = useState('')

  const [showGlobalTemplateForm, setShowGlobalTemplateForm] = useState(false)
  const [globalTemplateForm, setGlobalTemplateForm] = useState({ name: '', system_prompt: '', task_prompt: '' })
  const [editingTemplateID, setEditingTemplateID] = useState<number | null>(null)
  const createGlobalTemplate = useMutation({
    mutationFn: () => promptsApi.create({ ...globalTemplateForm, project_id: null }),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['global-templates'] }); setShowGlobalTemplateForm(false); setGlobalTemplateForm({ name: '', system_prompt: '', task_prompt: '' }) },
  })
  const updateGlobalTemplate = useMutation({
    mutationFn: () => promptsApi.update(editingTemplateID!, globalTemplateForm as Partial<PromptTemplate>),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['global-templates'] }); setEditingTemplateID(null); setShowGlobalTemplateForm(false); setGlobalTemplateForm({ name: '', system_prompt: '', task_prompt: '' }) },
  })
  const deleteGlobalTemplate = useMutation({
    mutationFn: (id: number) => promptsApi.delete(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['global-templates'] }),
  })
  const saveSetting = useMutation({
    mutationFn: (kv: Record<string, string>) => settingsApi.update(kv),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['settings'] }),
  })

  const [modelForm, setModelForm] = useState({ provider: '', model: '', supports_image: false, supports_resume: false, config_json: '{}' })
  const [showModelForm, setShowModelForm] = useState(false)
  const [editingModelID, setEditingModelID] = useState<number | null>(null)

  useEffect(() => {
    if (settings) {
      setForm({ ...settings })
      setAllowedUsers(parseJSONArray(settings.allowed_users))
      setLabelRules(parseJSONArray(settings.label_rules))
      setAgentProviders(parseJSONArray(settings.agent_providers))
      setRpOrigins(parseJSONArray(settings.rp_origins))
    }
  }, [settings])

  const updateField = (key: string, value: string) => setForm((prev) => ({ ...prev, [key]: value }))
  const saveKeys = (keys: Record<string, string>) => saveSetting.mutate(keys)
  const saveSection = (fieldKeys: string[]) => {
    const kv: Record<string, string> = {}
    for (const k of fieldKeys) if (form[k] !== undefined) kv[k] = form[k]
    saveKeys(kv)
  }

  const createModel = useMutation({
    mutationFn: () => modelsApi.create(modelForm),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['models'] }); setShowModelForm(false); setModelForm({ provider: '', model: '', supports_image: false, supports_resume: false, config_json: '{}' }) },
  })
  const updateModel = useMutation({
    mutationFn: () => modelsApi.update(editingModelID!, modelForm),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['models'] }); setEditingModelID(null); setShowModelForm(false); setModelForm({ provider: '', model: '', supports_image: false, supports_resume: false, config_json: '{}' }) },
  })
  const deleteModel = useMutation({
    mutationFn: (id: number) => modelsApi.delete(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['models'] }),
  })

  const handleRegisterPasskey = async () => {
    setPasskeyMsg('')
    try {
      const options = await authApi.passkeyRegisterStart() as Record<string, unknown>
      const pubKey = (options as { publicKey: Record<string, unknown> }).publicKey
      pubKey.challenge = b64ToBuffer(pubKey.challenge as string)
      ;(pubKey.user as Record<string, unknown>).id = b64ToBuffer((pubKey.user as Record<string, unknown>).id as string)
      const credential = await navigator.credentials.create({ publicKey: pubKey as unknown as PublicKeyCredentialCreationOptions }) as PublicKeyCredential
      if (!credential) { setPasskeyMsg('Cancelled.'); return }
      const response = credential.response as AuthenticatorAttestationResponse
      await authApi.passkeyRegisterFinish({
        id: credential.id, rawId: bufferToB64(credential.rawId), type: credential.type,
        response: { attestationObject: bufferToB64(response.attestationObject), clientDataJSON: bufferToB64(response.clientDataJSON) },
      })
      setPasskeyMsg('Passkey registered!')
      queryClient.invalidateQueries({ queryKey: ['me'] })
    } catch { setPasskeyMsg('Registration failed.') }
  }

  const providerOptions = agentProviders.map((p) => p.name).filter(Boolean)

  if (isLoading) return <div className="py-8 text-center text-gray-400 text-sm">Loading...</div>

  const field = (label: string, k: string, type = 'text', opts?: { help?: string; placeholder?: string }) => (
    <div key={k}>
      <Label>{label}</Label>
      <Input type={type} value={form[k] || ''} onChange={(e) => updateField(k, e.target.value)} placeholder={opts?.placeholder} />
      {opts?.help && <p className="text-xs text-gray-400 mt-1">{opts.help}</p>}
    </div>
  )

  return (
    <div className="max-w-4xl mx-auto">
      <h1 className="text-2xl font-bold mb-6">Settings</h1>

      {/* ====== GitHub OAuth ====== */}
      <Card>
        <CardHeader title="GitHub OAuth" description="Configure OAuth for user authentication" action={
          <a href="https://github.com/settings/developers" target="_blank" rel="noopener noreferrer" className="text-xs text-blue-600 hover:underline">Create OAuth App &rarr;</a>
        } />
        <CardContent>
          <Alert>
            Create an OAuth App, set <strong>Authorization callback URL</strong> to: <code className="px-1.5 py-0.5 bg-blue-100 rounded text-[11px]">{window.location.origin}/api/auth/github/callback</code>
          </Alert>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            {field("Client ID", "github_client_id")}
            {field("Client Secret", "github_client_secret", "password")}
          </div>
          {field("Callback Base URL", "github_callback_url", "text", { help: "Base URL only (e.g. https://ccmate.example.com). Path is appended automatically.", placeholder: window.location.origin })}

          <div>
            <Label>Allowed Users</Label>
            <div className="flex flex-wrap gap-2 mb-2">
              {allowedUsers.map((user, i) => (
                <Tag key={i} onRemove={() => setAllowedUsers(allowedUsers.filter((_, j) => j !== i))}>@{user}</Tag>
              ))}
              {allowedUsers.length === 0 && <span className="text-xs text-gray-400">No users added</span>}
            </div>
            <div className="flex gap-2 max-w-sm">
              <Input value={newUser} onChange={(e) => setNewUser(e.target.value)} placeholder="GitHub username"
                autoComplete="off" name="github-user-add" data-1p-ignore data-lpignore="true"
                onKeyDown={(e) => { if (e.key === 'Enter' && newUser.trim()) { e.preventDefault(); if (!allowedUsers.includes(newUser.trim())) setAllowedUsers([...allowedUsers, newUser.trim()]); setNewUser('') } }}
                className="flex-1" />
              <Btn variant="secondary" size="sm" onClick={() => { if (newUser.trim() && !allowedUsers.includes(newUser.trim())) { setAllowedUsers([...allowedUsers, newUser.trim()]); setNewUser('') } }}>Add</Btn>
            </div>
          </div>
        </CardContent>
        <CardFooter>
          <Btn onClick={() => saveKeys({ github_client_id: form.github_client_id || '', github_client_secret: form.github_client_secret || '', github_callback_url: form.github_callback_url || '', allowed_users: JSON.stringify(allowedUsers) })}>
            Save OAuth
          </Btn>
        </CardFooter>
      </Card>

      {/* ====== GitHub API ====== */}
      <Card>
        <CardHeader title="GitHub API" description="Token for repo operations and webhooks" action={
          <a href="https://github.com/settings/tokens?type=beta" target="_blank" rel="noopener noreferrer" className="text-xs text-blue-600 hover:underline">Create Token &rarr;</a>
        } />
        <CardContent>
          <Alert variant="warning">
            <strong>Required permissions</strong> (Fine-grained token):
            <span className="ml-1">Contents (R/W), Issues (R/W), Pull requests (R/W), Metadata (Read)</span>
          </Alert>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            {field("Personal Access Token", "github_personal_token", "password", { placeholder: "github_pat_..." })}
            {field("Webhook Secret", "github_webhook_secret", "password")}
          </div>

          <Separator />

          <div>
            <p className="text-sm font-medium text-gray-700 mb-3">GitHub App (optional)</p>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              {field("App ID", "github_app_id", "number")}
              {field("Installation ID", "github_installation_id", "number")}
              {field("Private Key Path", "github_private_key_path", "text", { placeholder: "/path/to/key.pem" })}
            </div>
          </div>

          <div className="flex items-center gap-3">
            <Btn variant="secondary" onClick={() => recheckPerms()} disabled={permsTesting}>
              {permsTesting ? 'Testing...' : 'Test Permissions'}
            </Btn>
            {ghPerms && (
              <span className={`text-sm ${ghPerms.valid ? 'text-green-600' : 'text-red-600'}`}>
                {ghPerms.valid ? <>Valid &middot; @{ghPerms.user}</> : <>{ghPerms.error || 'Invalid token'}</>}
              </span>
            )}
          </div>
        </CardContent>
        <CardFooter>
          <Btn onClick={() => saveSection(['github_personal_token', 'github_webhook_secret', 'github_app_id', 'github_installation_id', 'github_private_key_path'])}>Save API</Btn>
        </CardFooter>
      </Card>

      {/* ====== Agent Configuration ====== */}
      <Card>
        <CardHeader title="Agent Configuration" description="Configure coding agent binaries" />
        <CardContent>
          <div className="space-y-3">
            {agentProviders.map((p, i) => (
              <div key={i} className="flex items-center gap-3 p-3 rounded-lg border border-gray-200 bg-gray-50/50">
                <div className="flex-1 grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <div><Label>Provider</Label><Input value={p.name} onChange={(e) => { const u = [...agentProviders]; u[i] = { ...p, name: e.target.value }; setAgentProviders(u) }} /></div>
                  <div><Label>Binary Path</Label><Input value={p.binary} onChange={(e) => { const u = [...agentProviders]; u[i] = { ...p, binary: e.target.value }; setAgentProviders(u) }} /></div>
                </div>
                <Btn variant="ghost" size="sm" onClick={() => setAgentProviders(agentProviders.filter((_, j) => j !== i))} className="text-red-500 hover:text-red-700 hover:bg-red-50">&times;</Btn>
              </div>
            ))}
            <Btn variant="ghost" size="sm" onClick={() => setAgentProviders([...agentProviders, { name: '', binary: '' }])}>+ Add Provider</Btn>
          </div>
        </CardContent>
        <CardFooter><Btn onClick={() => saveKeys({ agent_providers: JSON.stringify(agentProviders) })}>Save Providers</Btn></CardFooter>
      </Card>

      {/* ====== Agent Profiles ====== */}
      <Card>
        <CardHeader title="Agent Profiles" description="Model configurations for task execution" action={
          <Btn variant="ghost" size="sm" onClick={() => {
            if (showModelForm) { setShowModelForm(false); setEditingModelID(null); setModelForm({ provider: '', model: '', supports_image: false, supports_resume: false, config_json: '{}' }) }
            else setShowModelForm(true)
          }}>{showModelForm ? 'Cancel' : '+ New'}</Btn>
        } />
        <CardContent>
          {showModelForm && (
            <div className="p-4 rounded-lg border border-blue-200 bg-blue-50/30 space-y-3">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                {providerOptions.length > 0 ? (
                  <div><Label>Provider</Label><Select value={modelForm.provider} onChange={(e) => setModelForm({ ...modelForm, provider: e.target.value })} className="w-full">
                    <option value="">Select provider</option>
                    {providerOptions.map((p) => <option key={p} value={p}>{p}</option>)}
                  </Select></div>
                ) : (
                  <div><Label>Provider</Label><Input value={modelForm.provider} onChange={(e) => setModelForm({ ...modelForm, provider: e.target.value })} placeholder="claude-code" /></div>
                )}
                <div><Label>Model</Label><Input value={modelForm.model} onChange={(e) => setModelForm({ ...modelForm, model: e.target.value })} placeholder="claude-sonnet-4-20250514" /></div>
              </div>
              <div><Label>Config JSON</Label><textarea value={modelForm.config_json} onChange={(e) => setModelForm({ ...modelForm, config_json: e.target.value })} rows={2} className="w-full px-3 py-2 rounded-lg border border-gray-300 text-sm font-mono bg-white shadow-sm focus:outline-none focus:ring-2 focus:ring-blue-500/20 focus:border-blue-500" /></div>
              <div className="flex gap-4">
                <Checkbox label="Image" checked={modelForm.supports_image} onChange={(v) => setModelForm({ ...modelForm, supports_image: v })} />
                <Checkbox label="Resume" checked={modelForm.supports_resume} onChange={(v) => setModelForm({ ...modelForm, supports_resume: v })} />
              </div>
              <div className="flex gap-2">
                <Btn onClick={() => editingModelID ? updateModel.mutate() : createModel.mutate()} disabled={!modelForm.provider || !modelForm.model}>
                  {editingModelID ? 'Save' : 'Create'}
                </Btn>
              </div>
            </div>
          )}

          <div>
            <Label>Default Agent</Label>
            <Select value={form.default_agent_profile_id || ''} onChange={(e) => { updateField('default_agent_profile_id', e.target.value); saveSetting.mutate({ default_agent_profile_id: e.target.value }) }} className="w-full max-w-md">
              <option value="">None</option>
              {(models || []).map((m: AgentProfile) => <option key={m.id} value={String(m.id)}>{m.provider} / {m.model}</option>)}
            </Select>
            <p className="text-xs text-gray-400 mt-1">Fallback when task/project has no agent.</p>
          </div>

          {models && models.length > 0 ? (
            <div className="space-y-2">
              {models.map((m: AgentProfile) => (
                <div key={m.id} className="flex items-center justify-between p-3 rounded-lg border border-gray-200 hover:border-gray-300 transition-colors">
                  <div className="flex items-center gap-3">
                    <span className="inline-flex items-center rounded-md bg-purple-50 px-2 py-1 text-xs font-medium text-purple-700 border border-purple-200">{m.provider}</span>
                    <span className="text-sm font-mono text-gray-700">{m.model}</span>
                    {m.supports_image && <span className="text-xs text-gray-400">img</span>}
                    {m.supports_resume && <span className="text-xs text-gray-400">resume</span>}
                  </div>
                  <div className="flex items-center gap-2">
                    <Btn variant="ghost" size="sm" onClick={() => {
                      setEditingModelID(m.id); setShowModelForm(true)
                      setModelForm({ provider: m.provider, model: m.model, supports_image: m.supports_image, supports_resume: m.supports_resume, config_json: m.config_json || '{}' })
                    }}>Edit</Btn>
                    <Btn variant="ghost" size="sm" onClick={() => deleteModel.mutate(m.id)} className="text-red-500 hover:text-red-700 hover:bg-red-50">Delete</Btn>
                  </div>
                </div>
              ))}
            </div>
          ) : !showModelForm && <p className="text-sm text-gray-400">No agent profiles configured.</p>}
        </CardContent>
      </Card>

      {/* ====== Prompt Templates ====== */}
      <Card>
        <CardHeader title="Prompt Templates" description="Global prompt configurations" action={
          <Btn variant="ghost" size="sm" onClick={() => {
            if (showGlobalTemplateForm) { setShowGlobalTemplateForm(false); setEditingTemplateID(null); setGlobalTemplateForm({ name: '', system_prompt: '', task_prompt: '' }) }
            else setShowGlobalTemplateForm(true)
          }}>{showGlobalTemplateForm ? 'Cancel' : '+ New'}</Btn>
        } />
        <CardContent>
          {showGlobalTemplateForm && (
            <div className="p-4 rounded-lg border border-blue-200 bg-blue-50/30 space-y-3">
              <div><Label>Template Name</Label><Input value={globalTemplateForm.name} onChange={(e) => setGlobalTemplateForm({ ...globalTemplateForm, name: e.target.value })} placeholder="Template name" /></div>
              <PromptEditor label="System Prompt" value={globalTemplateForm.system_prompt} onChange={(v) => setGlobalTemplateForm({ ...globalTemplateForm, system_prompt: v })} placeholder="System prompt..." rows={4} />
              <PromptEditor label="Task Prompt Template" value={globalTemplateForm.task_prompt} onChange={(v) => setGlobalTemplateForm({ ...globalTemplateForm, task_prompt: v })} placeholder="Task prompt (supports {{.IssueTitle}} etc.)" rows={4} showVars />
              <Btn onClick={() => editingTemplateID ? updateGlobalTemplate.mutate() : createGlobalTemplate.mutate()} disabled={!globalTemplateForm.name}>
                {editingTemplateID ? 'Save' : 'Create'}
              </Btn>
            </div>
          )}

          <div>
            <Label>Default Template</Label>
            <Select value={form.default_prompt_template_id || ''} onChange={(e) => { updateField('default_prompt_template_id', e.target.value); saveSetting.mutate({ default_prompt_template_id: e.target.value }) }} className="w-full max-w-md">
              <option value="">None</option>
              {(globalTemplates || []).map((t: PromptTemplate) => <option key={t.id} value={String(t.id)}>{t.name}</option>)}
            </Select>
          </div>

          {globalTemplates && globalTemplates.length > 0 ? (
            <div className="space-y-2">
              {globalTemplates.map((t: PromptTemplate) => (
                <div key={t.id} className="flex items-center justify-between p-3 rounded-lg border border-gray-200 hover:border-gray-300 transition-colors">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">{t.name}</span>
                    {t.is_builtin && <span className="text-xs text-gray-400">(builtin)</span>}
                    {form.default_prompt_template_id === String(t.id) && <Tag color="blue">Default</Tag>}
                  </div>
                  <div className="flex items-center gap-2">
                    {form.default_prompt_template_id !== String(t.id) && (
                      <Btn variant="ghost" size="sm" onClick={() => { updateField('default_prompt_template_id', String(t.id)); saveSetting.mutate({ default_prompt_template_id: String(t.id) }) }}>Set Default</Btn>
                    )}
                    <Btn variant="ghost" size="sm" onClick={() => {
                      setEditingTemplateID(t.id); setShowGlobalTemplateForm(true)
                      setGlobalTemplateForm({ name: t.name, system_prompt: t.system_prompt, task_prompt: t.task_prompt })
                    }}>Edit</Btn>
                    {!t.is_builtin && <Btn variant="ghost" size="sm" onClick={() => deleteGlobalTemplate.mutate(t.id)} className="text-red-500 hover:text-red-700 hover:bg-red-50">Delete</Btn>}
                  </div>
                </div>
              ))}
            </div>
          ) : !showGlobalTemplateForm && <p className="text-sm text-gray-400">No templates yet.</p>}
        </CardContent>
      </Card>

      {/* ====== Label Rules ====== */}
      <Card>
        <CardHeader title="Label Rules" description="Auto-trigger tasks based on issue labels" />
        <CardContent>
          <div className="space-y-2">
            {labelRules.map((rule, i) => (
              <div key={i} className="flex items-center gap-3 p-3 rounded-lg border border-gray-200">
                <Tag>{rule.label}</Tag>
                <Select value={rule.trigger_mode} onChange={(e) => { const u = [...labelRules]; u[i] = { ...rule, trigger_mode: e.target.value }; setLabelRules(u) }} className="w-24">
                  <option value="auto">auto</option>
                  <option value="manual">manual</option>
                </Select>
                <Btn variant="ghost" size="sm" onClick={() => setLabelRules(labelRules.filter((_, j) => j !== i))} className="ml-auto text-red-500 hover:text-red-700 hover:bg-red-50">&times;</Btn>
              </div>
            ))}
          </div>
          <div className="flex gap-2">
            <Input value={newLabel} onChange={(e) => setNewLabel(e.target.value)} placeholder="Label name" className="flex-1"
              onKeyDown={(e) => { if (e.key === 'Enter' && newLabel.trim()) { setLabelRules([...labelRules, { label: newLabel.trim(), trigger_mode: newTrigger }]); setNewLabel('') } }} />
            <Select value={newTrigger} onChange={(e) => setNewTrigger(e.target.value)} className="w-24">
              <option value="auto">auto</option>
              <option value="manual">manual</option>
            </Select>
            <Btn variant="secondary" size="sm" onClick={() => { if (newLabel.trim()) { setLabelRules([...labelRules, { label: newLabel.trim(), trigger_mode: newTrigger }]); setNewLabel('') } }}>Add</Btn>
          </div>
        </CardContent>
        <CardFooter><Btn onClick={() => saveKeys({ label_rules: JSON.stringify(labelRules) })}>Save Rules</Btn></CardFooter>
      </Card>

      {/* ====== Limits ====== */}
      <Card>
        <CardHeader title="Limits" description="Concurrency and resource constraints" />
        <CardContent>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
            {field("Max Concurrency", "max_concurrency", "number")}
            {field("Timeout (min)", "task_timeout_minutes", "number")}
            {field("Max Log (MB)", "max_log_size_mb", "number")}
            {field("Max Attachment (MB)", "max_attachment_size_mb", "number")}
          </div>
        </CardContent>
        <CardFooter><Btn onClick={() => saveSection(['max_concurrency', 'task_timeout_minutes', 'max_log_size_mb', 'max_attachment_size_mb'])}>Save Limits</Btn></CardFooter>
      </Card>

      {/* ====== Notifications ====== */}
      <Card>
        <CardHeader title="Notifications" description="Task status change alerts" />
        <CardContent>
          {field("Base URL", "notify_base_url", "text", { placeholder: "https://ccmate.example.com", help: "Used for task links in notifications" })}

          <div>
            <Label>Notify on status changes</Label>
            <div className="flex flex-wrap gap-x-4 gap-y-2">
              {['queued', 'running', 'succeeded', 'failed', 'waiting_user', 'paused', 'cancelled'].map((s) => {
                const statuses: string[] = parseJSONArray(form.notify_enabled_statuses)
                return (
                  <Checkbox key={s} label={s} checked={statuses.includes(s)} onChange={(checked) => {
                    const next = checked ? [...statuses, s] : statuses.filter((x) => x !== s)
                    updateField('notify_enabled_statuses', JSON.stringify(next))
                  }} />
                )
              })}
            </div>
          </div>

          <Separator />

          <div>
            <p className="text-sm font-medium text-gray-700 mb-3">Telegram</p>
            <div className="space-y-3">
              <Checkbox label="Enable Telegram notifications" checked={form.notify_telegram_enabled === 'true'} onChange={(v) => updateField('notify_telegram_enabled', v ? 'true' : 'false')} />
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                {field("Bot Token", "notify_telegram_bot_token", "password", { placeholder: "123456:ABC-DEF..." })}
                {field("Chat ID", "notify_telegram_chat_id", "text", { placeholder: "-1001234567890" })}
              </div>
            </div>
          </div>
        </CardContent>
        <CardFooter>
          <Btn onClick={() => saveKeys({
            notify_base_url: form.notify_base_url || '', notify_enabled_statuses: form.notify_enabled_statuses || '[]',
            notify_telegram_enabled: form.notify_telegram_enabled || 'false', notify_telegram_bot_token: form.notify_telegram_bot_token || '', notify_telegram_chat_id: form.notify_telegram_chat_id || '',
          })}>Save Notifications</Btn>
          <Btn variant="secondary" onClick={() => { settingsApi.testNotification().then(() => toast('Test notification sent!', 'success')).catch((e) => toast('Test failed: ' + e.message, 'error')) }}>Test</Btn>
        </CardFooter>
      </Card>

      {/* ====== Debug ====== */}
      <Card>
        <CardHeader title="Debug" />
        <CardContent className="!py-4">
          <Checkbox label="Enable Debug Mode" checked={form.debug_mode === 'true'} description="Logs full agent command arguments to task events"
            onChange={(v) => { const val = v ? 'true' : 'false'; updateField('debug_mode', val); saveKeys({ debug_mode: val }) }} />
        </CardContent>
      </Card>

      {/* ====== WebAuthn ====== */}
      <Card>
        <CardHeader title="WebAuthn / Passkey" />
        <CardContent>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            {field("RP Display Name", "rp_display_name")}
            {field("RP ID", "rp_id", "text", { help: "Usually your domain (e.g. ccmate.example.com)" })}
          </div>
          <div>
            <Label>RP Origins</Label>
            <div className="flex flex-wrap gap-2 mb-2">
              {rpOrigins.map((origin, i) => (
                <Tag key={i} color="gray" onRemove={() => setRpOrigins(rpOrigins.filter((_, j) => j !== i))}>{origin}</Tag>
              ))}
            </div>
            <div className="flex gap-2">
              <Input value={newOrigin} onChange={(e) => setNewOrigin(e.target.value)} placeholder="https://ccmate.example.com" className="flex-1"
                onKeyDown={(e) => { if (e.key === 'Enter' && newOrigin.trim()) { setRpOrigins([...rpOrigins, newOrigin.trim()]); setNewOrigin('') } }} />
              <Btn variant="secondary" size="sm" onClick={() => { if (newOrigin.trim()) { setRpOrigins([...rpOrigins, newOrigin.trim()]); setNewOrigin('') } }}>Add</Btn>
            </div>
          </div>

          <Separator />

          <div>
            {passkeyMsg && <p className="text-sm text-blue-600 mb-2">{passkeyMsg}</p>}
            {me?.has_passkey ? (
              <div className="flex items-center gap-4">
                <span className="inline-flex items-center gap-1.5 text-sm text-green-600 font-medium">
                  <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" /></svg>
                  Passkey configured
                </span>
                <Btn variant="danger" size="sm" onClick={async () => { await authApi.passkeyRemove(); setPasskeyMsg('Removed.'); queryClient.invalidateQueries({ queryKey: ['me'] }) }}>Remove</Btn>
              </div>
            ) : (
              <Btn onClick={handleRegisterPasskey}>Register Passkey</Btn>
            )}
          </div>
        </CardContent>
        <CardFooter>
          <Btn onClick={() => saveKeys({ rp_display_name: form.rp_display_name || '', rp_id: form.rp_id || '', rp_origins: JSON.stringify(rpOrigins) })}>Save WebAuthn</Btn>
        </CardFooter>
      </Card>

      {/* ====== Storage ====== */}
      <Card>
        <CardHeader title="Storage" />
        <CardContent>
          {field("Base Path", "storage_base_path", "text", { help: "Base directory for data, workspaces, logs" })}
        </CardContent>
        <CardFooter><Btn onClick={() => saveSection(['storage_base_path'])}>Save Storage</Btn></CardFooter>
      </Card>

      {/* ====== Commands ====== */}
      <Card>
        <CardHeader title="Comment Commands" description="GitHub issue/PR comment commands" />
        <CardContent className="!py-4">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-1.5">
            {['run - Start a task', 'pause - Pause active task', 'resume - Resume paused task', 'retry - Retry failed task', 'status - Show task status', 'fix-review - Trigger review fix'].map((cmd) => (
              <div key={cmd} className="flex items-center gap-2 text-sm py-1">
                <code className="px-1.5 py-0.5 bg-gray-100 rounded text-xs font-mono text-gray-700">/ccmate {cmd.split(' - ')[0]}</code>
                <span className="text-gray-400">{cmd.split(' - ')[1]}</span>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function bufferToB64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer); let str = ''
  for (const b of bytes) str += String.fromCharCode(b)
  return btoa(str).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
}
function b64ToBuffer(b64: string): ArrayBuffer {
  const base64 = b64.replace(/-/g, '+').replace(/_/g, '/')
  const padded = base64 + '='.repeat((4 - (base64.length % 4)) % 4)
  const binary = atob(padded)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return bytes.buffer
}
