import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, useEffect } from 'react'
import { settingsApi, modelsApi, authApi, type AgentProfile } from '../lib/api'

// Helper: parse JSON array from settings string, fallback to []
function parseJSONArray<T>(val: string | undefined): T[] {
  if (!val) return []
  try { return JSON.parse(val) } catch { return [] }
}

export default function SettingsPage() {
  const queryClient = useQueryClient()
  const { data: settings, isLoading } = useQuery({ queryKey: ['settings'], queryFn: settingsApi.get })
  const { data: models } = useQuery({ queryKey: ['models'], queryFn: modelsApi.list })
  const { data: me } = useQuery({ queryKey: ['me'], queryFn: authApi.me })
  const { data: ghPerms, refetch: recheckPerms, isFetching: permsTesting } = useQuery({
    queryKey: ['gh-perms'], queryFn: settingsApi.checkGitHubPermissions, enabled: true,
  })

  const [form, setForm] = useState<Record<string, string>>({})
  const [dirty, setDirty] = useState(false)
  const [saveMsg, setSaveMsg] = useState('')
  const [passkeyMsg, setPasskeyMsg] = useState('')

  // Visual state for array/object fields
  const [allowedUsers, setAllowedUsers] = useState<string[]>([])
  const [newUser, setNewUser] = useState('')
  const [labelRules, setLabelRules] = useState<{ label: string; trigger_mode: string }[]>([])
  const [newLabel, setNewLabel] = useState('')
  const [newTrigger, setNewTrigger] = useState('auto')
  const [agentProviders, setAgentProviders] = useState<{ name: string; binary: string }[]>([])
  const [rpOrigins, setRpOrigins] = useState<string[]>([])
  const [newOrigin, setNewOrigin] = useState('')

  // Model form
  const [modelForm, setModelForm] = useState({ provider: '', model: '', supports_image: false, supports_resume: false, config_json: '{}' })
  const [showModelForm, setShowModelForm] = useState(false)

  useEffect(() => {
    if (settings) {
      setForm({ ...settings })
      setAllowedUsers(parseJSONArray(settings.allowed_users))
      setLabelRules(parseJSONArray(settings.label_rules))
      setAgentProviders(parseJSONArray(settings.agent_providers))
      setRpOrigins(parseJSONArray(settings.rp_origins))
      setDirty(false)
    }
  }, [settings])

  const updateField = (key: string, value: string) => {
    setForm((prev) => ({ ...prev, [key]: value }))
    setDirty(true)
  }

  // Sync visual arrays back to form before save
  const buildSaveForm = () => ({
    ...form,
    allowed_users: JSON.stringify(allowedUsers),
    label_rules: JSON.stringify(labelRules),
    agent_providers: JSON.stringify(agentProviders),
    rp_origins: JSON.stringify(rpOrigins),
  })

  const saveMutation = useMutation({
    mutationFn: () => settingsApi.update(buildSaveForm()),
    onSuccess: () => {
      setSaveMsg('Settings saved!')
      setDirty(false)
      queryClient.invalidateQueries({ queryKey: ['settings'] })
      setTimeout(() => setSaveMsg(''), 3000)
    },
    onError: (e) => setSaveMsg(`Error: ${e.message}`),
  })

  const createModel = useMutation({
    mutationFn: () => modelsApi.create(modelForm),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['models'] }); setShowModelForm(false); setModelForm({ provider: '', model: '', supports_image: false, supports_resume: false, config_json: '{}' }) },
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

  if (isLoading) return <div className="text-gray-500">Loading...</div>

  const field = (label: string, k: string, type = 'text', help?: string, placeholder?: string) => (
    <div key={k}>
      <label className="block text-xs font-medium text-gray-600 mb-1">{label}</label>
      <input type={type} value={form[k] || ''} onChange={(e) => updateField(k, e.target.value)} placeholder={placeholder}
        className="w-full px-3 py-1.5 border rounded text-sm" />
      {help && <p className="text-xs text-gray-400 mt-0.5">{help}</p>}
    </div>
  )

  return (
    <div>
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-2xl font-bold">Settings</h1>
        <div className="flex items-center gap-3">
          {saveMsg && <span className="text-sm text-green-600">{saveMsg}</span>}
          <button onClick={() => saveMutation.mutate()} disabled={!dirty || saveMutation.isPending}
            className="px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-700 disabled:opacity-50">
            {saveMutation.isPending ? 'Saving...' : 'Save All Changes'}
          </button>
        </div>
      </div>

      {/* ====== GitHub OAuth ====== */}
      <Section title="GitHub OAuth" link="https://github.com/settings/developers" linkText="Create OAuth App">
        <p className="text-xs text-gray-500 mb-3">
          Create an OAuth App, set <strong>Authorization callback URL</strong> to: <code className="px-1 bg-gray-100 rounded">{window.location.origin}/api/auth/github/callback</code>
        </p>
        <div className="grid grid-cols-2 gap-4">
          {field("Client ID", "github_client_id")}
          {field("Client Secret", "github_client_secret", "password")}
        </div>
        {field("Callback Base URL", "github_callback_url", "text", "Only the base URL (e.g. https://ccmate.example.com). The path /api/auth/github/callback is appended automatically.", window.location.origin)}

        {/* Allowed Users - visual tag editor */}
        <div>
          <label className="block text-xs font-medium text-gray-600 mb-1">Allowed Users</label>
          <div className="flex flex-wrap gap-2 mb-2">
            {allowedUsers.map((user, i) => (
              <span key={i} className="inline-flex items-center gap-1 px-2 py-1 bg-blue-100 text-blue-700 rounded text-xs">
                @{user}
                <button onClick={() => { setAllowedUsers(allowedUsers.filter((_, j) => j !== i)); setDirty(true) }}
                  className="text-blue-500 hover:text-red-500">&times;</button>
              </span>
            ))}
          </div>
          <div className="flex gap-2">
            <input value={newUser} onChange={(e) => setNewUser(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && newUser.trim()) {
                  e.preventDefault()
                  if (!allowedUsers.includes(newUser.trim())) {
                    setAllowedUsers([...allowedUsers, newUser.trim()])
                    setDirty(true)
                  }
                  setNewUser('')
                }
              }}
              placeholder="GitHub username" className="flex-1 px-3 py-1.5 border rounded text-sm" />
            <button onClick={() => {
              if (newUser.trim() && !allowedUsers.includes(newUser.trim())) {
                setAllowedUsers([...allowedUsers, newUser.trim()])
                setDirty(true)
                setNewUser('')
              }
            }} className="px-3 py-1.5 bg-gray-100 rounded text-sm hover:bg-gray-200">Add</button>
          </div>
        </div>
      </Section>

      {/* ====== GitHub API ====== */}
      <Section title="GitHub API" link="https://github.com/settings/tokens?type=beta" linkText="Create Fine-grained Token">
        <div className="p-3 bg-amber-50 border border-amber-200 rounded text-xs text-amber-800 mb-3">
          <strong>Required permissions</strong> (Fine-grained token, select target repos):
          <ul className="mt-1 ml-4 list-disc space-y-0.5">
            <li><strong>Contents</strong>: Read and write</li>
            <li><strong>Issues</strong>: Read and write</li>
            <li><strong>Pull requests</strong>: Read and write</li>
            <li><strong>Metadata</strong>: Read-only</li>
          </ul>
        </div>
        <div className="grid grid-cols-2 gap-4">
          {field("Personal Access Token", "github_personal_token", "password", undefined, "github_pat_...")}
          {field("Webhook Secret", "github_webhook_secret", "password")}
        </div>

        <h3 className="text-sm font-semibold mt-4 mb-2">GitHub App (optional, for production)</h3>
        <div className="grid grid-cols-3 gap-4">
          {field("App ID", "github_app_id", "number")}
          {field("Installation ID", "github_installation_id", "number")}
          {field("Private Key Path", "github_private_key_path", "text", undefined, "/path/to/key.pem")}
        </div>

        {/* Token test */}
        <div className="mt-4 pt-3 border-t">
          <div className="flex items-center gap-3">
            <button onClick={() => recheckPerms()} disabled={permsTesting}
              className="px-3 py-1.5 border rounded text-sm hover:bg-gray-50 disabled:opacity-50">
              {permsTesting ? 'Testing...' : 'Test Token Permissions'}
            </button>
            {ghPerms && (
              <span className={`text-sm ${ghPerms.valid ? 'text-green-600' : 'text-red-600'}`}>
                {ghPerms.valid
                  ? <>Valid &middot; @{ghPerms.user}{ghPerms.scopes ? ` &middot; ${ghPerms.scopes}` : ''}</>
                  : <>{ghPerms.error || 'Invalid token'}</>}
              </span>
            )}
            {!ghPerms && !permsTesting && <span className="text-xs text-gray-400">Checking...</span>}
          </div>
        </div>
      </Section>

      {/* ====== Agent Configuration ====== */}
      <Section title="Agent Configuration">
        <div className="space-y-2">
          {agentProviders.map((p, i) => (
            <div key={i} className="flex items-center gap-3 p-3 bg-gray-50 rounded">
              <div className="flex-1 grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs text-gray-500">Provider Name</label>
                  <input value={p.name} onChange={(e) => {
                    const updated = [...agentProviders]; updated[i] = { ...p, name: e.target.value }
                    setAgentProviders(updated); setDirty(true)
                  }} className="w-full px-2 py-1 border rounded text-sm" />
                </div>
                <div>
                  <label className="block text-xs text-gray-500">Binary Path</label>
                  <input value={p.binary} onChange={(e) => {
                    const updated = [...agentProviders]; updated[i] = { ...p, binary: e.target.value }
                    setAgentProviders(updated); setDirty(true)
                  }} className="w-full px-2 py-1 border rounded text-sm" />
                </div>
              </div>
              <button onClick={() => { setAgentProviders(agentProviders.filter((_, j) => j !== i)); setDirty(true) }}
                className="text-red-500 text-sm hover:underline shrink-0">Remove</button>
            </div>
          ))}
          <button onClick={() => { setAgentProviders([...agentProviders, { name: '', binary: '' }]); setDirty(true) }}
            className="text-sm text-blue-600 hover:underline">+ Add Provider</button>
        </div>
      </Section>

      {/* ====== Agent Profiles (DB) ====== */}
      <div className="bg-white rounded-lg shadow p-6 mb-6">
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-lg font-semibold">Agent Profiles</h2>
          <button onClick={() => setShowModelForm(!showModelForm)} className="text-sm text-blue-600">{showModelForm ? 'Cancel' : '+ New'}</button>
        </div>
        {showModelForm && (
          <div className="mb-4 p-4 bg-gray-50 rounded space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <input value={modelForm.provider} onChange={(e) => setModelForm({...modelForm, provider: e.target.value})} placeholder="Provider" className="px-3 py-1.5 border rounded text-sm" />
              <input value={modelForm.model} onChange={(e) => setModelForm({...modelForm, model: e.target.value})} placeholder="Model" className="px-3 py-1.5 border rounded text-sm" />
            </div>
            <div className="flex gap-4">
              <label className="flex items-center gap-1 text-sm"><input type="checkbox" checked={modelForm.supports_image} onChange={(e) => setModelForm({...modelForm, supports_image: e.target.checked})} /> Image</label>
              <label className="flex items-center gap-1 text-sm"><input type="checkbox" checked={modelForm.supports_resume} onChange={(e) => setModelForm({...modelForm, supports_resume: e.target.checked})} /> Resume</label>
            </div>
            <button onClick={() => createModel.mutate()} disabled={!modelForm.provider || !modelForm.model} className="px-3 py-1.5 bg-blue-600 text-white rounded text-sm disabled:opacity-50">Create</button>
          </div>
        )}
        {models && models.length > 0 ? (
          <table className="min-w-full divide-y divide-gray-200">
            <thead><tr>
              {['Provider','Model','Image','Resume',''].map((h) => <th key={h} className="text-left text-xs font-medium text-gray-500 uppercase py-2">{h}</th>)}
            </tr></thead>
            <tbody className="divide-y divide-gray-200">
              {models.map((m: AgentProfile) => (
                <tr key={m.id}>
                  <td className="py-2 text-sm">{m.provider}</td>
                  <td className="py-2 text-sm">{m.model}</td>
                  <td className="py-2 text-sm">{m.supports_image ? 'Yes' : 'No'}</td>
                  <td className="py-2 text-sm">{m.supports_resume ? 'Yes' : 'No'}</td>
                  <td className="py-2 text-sm"><button onClick={() => deleteModel.mutate(m.id)} className="text-red-500 text-xs hover:underline">Delete</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : !showModelForm && <p className="text-sm text-gray-500">No agent profiles.</p>}
      </div>

      {/* ====== Label Rules ====== */}
      <Section title="Label Rules">
        <div className="space-y-2 mb-3">
          {labelRules.map((rule, i) => (
            <div key={i} className="flex items-center gap-3 py-2 px-3 bg-gray-50 rounded">
              <span className="px-2 py-0.5 bg-blue-100 text-blue-700 rounded text-xs font-medium">{rule.label}</span>
              <select value={rule.trigger_mode} onChange={(e) => {
                const updated = [...labelRules]; updated[i] = { ...rule, trigger_mode: e.target.value }
                setLabelRules(updated); setDirty(true)
              }} className="px-2 py-0.5 border rounded text-xs">
                <option value="auto">auto</option>
                <option value="manual">manual</option>
              </select>
              <button onClick={() => { setLabelRules(labelRules.filter((_, j) => j !== i)); setDirty(true) }}
                className="ml-auto text-red-500 text-xs hover:underline">Remove</button>
            </div>
          ))}
        </div>
        <div className="flex gap-2">
          <input value={newLabel} onChange={(e) => setNewLabel(e.target.value)} placeholder="Label name"
            onKeyDown={(e) => { if (e.key === 'Enter' && newLabel.trim()) { setLabelRules([...labelRules, { label: newLabel.trim(), trigger_mode: newTrigger }]); setNewLabel(''); setDirty(true) } }}
            className="flex-1 px-3 py-1.5 border rounded text-sm" />
          <select value={newTrigger} onChange={(e) => setNewTrigger(e.target.value)} className="px-3 py-1.5 border rounded text-sm">
            <option value="auto">auto</option>
            <option value="manual">manual</option>
          </select>
          <button onClick={() => { if (newLabel.trim()) { setLabelRules([...labelRules, { label: newLabel.trim(), trigger_mode: newTrigger }]); setNewLabel(''); setDirty(true) } }}
            className="px-3 py-1.5 bg-gray-100 rounded text-sm hover:bg-gray-200">Add</button>
        </div>
      </Section>

      {/* ====== Limits ====== */}
      <Section title="Limits">
        <div className="grid grid-cols-2 gap-4">
          {field("Max Global Concurrency", "max_concurrency", "number")}
          {field("Task Timeout (minutes)", "task_timeout_minutes", "number")}
          {field("Max Log Size (MB)", "max_log_size_mb", "number")}
          {field("Max Attachment Size (MB)", "max_attachment_size_mb", "number")}
        </div>
      </Section>

      {/* ====== Debug ====== */}
      <Section title="Debug">
        <div className="flex items-center gap-3">
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox"
              checked={form.debug_mode === 'true'}
              onChange={(e) => updateField('debug_mode', e.target.checked ? 'true' : 'false')} />
            Enable Debug Mode
          </label>
          <span className="text-xs text-gray-400">Logs full claude command arguments and parameters to task events</span>
        </div>
      </Section>

      {/* ====== WebAuthn / Passkey ====== */}
      <Section title="WebAuthn / Passkey">
        <div className="grid grid-cols-2 gap-4">
          {field("RP Display Name", "rp_display_name")}
          {field("RP ID", "rp_id", "text", "Usually your domain name (e.g. ccmate.example.com)")}
        </div>
        {/* RP Origins - visual tag editor */}
        <div>
          <label className="block text-xs font-medium text-gray-600 mb-1">RP Origins</label>
          <div className="flex flex-wrap gap-2 mb-2">
            {rpOrigins.map((origin, i) => (
              <span key={i} className="inline-flex items-center gap-1 px-2 py-1 bg-gray-100 text-gray-700 rounded text-xs">
                {origin}
                <button onClick={() => { setRpOrigins(rpOrigins.filter((_, j) => j !== i)); setDirty(true) }}
                  className="text-gray-500 hover:text-red-500">&times;</button>
              </span>
            ))}
          </div>
          <div className="flex gap-2">
            <input value={newOrigin} onChange={(e) => setNewOrigin(e.target.value)} placeholder="https://ccmate.example.com"
              onKeyDown={(e) => { if (e.key === 'Enter' && newOrigin.trim()) { setRpOrigins([...rpOrigins, newOrigin.trim()]); setNewOrigin(''); setDirty(true) } }}
              className="flex-1 px-3 py-1.5 border rounded text-sm" />
            <button onClick={() => { if (newOrigin.trim()) { setRpOrigins([...rpOrigins, newOrigin.trim()]); setNewOrigin(''); setDirty(true) } }}
              className="px-3 py-1.5 bg-gray-100 rounded text-sm hover:bg-gray-200">Add</button>
          </div>
        </div>
        <div className="mt-4 pt-4 border-t">
          {passkeyMsg && <div className="mb-2 text-sm text-blue-600">{passkeyMsg}</div>}
          {me?.has_passkey ? (
            <div className="flex items-center gap-4">
              <span className="text-sm text-green-600 font-medium">Passkey configured</span>
              <button onClick={async () => { await authApi.passkeyRemove(); setPasskeyMsg('Removed.'); queryClient.invalidateQueries({ queryKey: ['me'] }) }}
                className="text-sm text-red-500 hover:underline">Remove</button>
            </div>
          ) : (
            <button onClick={handleRegisterPasskey} className="px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-700">Register Passkey</button>
          )}
        </div>
      </Section>

      {/* ====== Storage ====== */}
      <Section title="Storage">
        {field("Base Path", "storage_base_path", "text", "Base directory for data, attachments, logs, workspaces")}
      </Section>

      {/* ====== Commands ====== */}
      <div className="bg-white rounded-lg shadow p-6">
        <h2 className="text-lg font-semibold mb-4">Comment Commands</h2>
        <ul className="text-sm space-y-1 text-gray-700">
          {['run - Start a task', 'pause - Pause active task', 'resume - Resume paused task', 'retry - Retry failed task', 'status - Show task status', 'fix-review - Trigger review fix'].map((cmd) => (
            <li key={cmd}><code className="bg-gray-100 px-1 rounded">/ccmate {cmd.split(' - ')[0]}</code> <span className="text-gray-500">— {cmd.split(' - ')[1]}</span></li>
          ))}
        </ul>
      </div>
    </div>
  )
}

function Section({ title, link, linkText, children }: { title: string; link?: string; linkText?: string; children: React.ReactNode }) {
  return (
    <div className="bg-white rounded-lg shadow p-6 mb-6">
      <div className="flex justify-between items-center mb-4">
        <h2 className="text-lg font-semibold">{title}</h2>
        {link && <a href={link} target="_blank" rel="noopener noreferrer" className="text-xs text-blue-600 hover:underline">{linkText} &rarr;</a>}
      </div>
      <div className="space-y-3">{children}</div>
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
