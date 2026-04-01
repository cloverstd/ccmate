import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { modelsApi, authApi, type AgentProfile } from '../lib/api'

export default function SettingsPage() {
  const queryClient = useQueryClient()
  const [showForm, setShowForm] = useState(false)
  const [form, setForm] = useState({ provider: '', model: '', supports_image: false, supports_resume: false, config_json: '{}' })
  const [passkeyMsg, setPasskeyMsg] = useState('')

  const { data: models } = useQuery({ queryKey: ['models'], queryFn: modelsApi.list })
  const { data: me } = useQuery({ queryKey: ['me'], queryFn: authApi.me })

  const createMutation = useMutation({
    mutationFn: () => modelsApi.create(form),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['models'] }); setShowForm(false); setForm({ provider: '', model: '', supports_image: false, supports_resume: false, config_json: '{}' }) },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => modelsApi.delete(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['models'] }),
  })

  const handleRegisterPasskey = async () => {
    setPasskeyMsg('')
    try {
      const options = await authApi.passkeyRegisterStart() as Record<string, unknown>
      const pubKey = (options as { publicKey: Record<string, unknown> }).publicKey
      pubKey.challenge = base64urlToBuffer(pubKey.challenge as string)
      ;(pubKey.user as Record<string, unknown>).id = base64urlToBuffer((pubKey.user as Record<string, unknown>).id as string)

      const credential = await navigator.credentials.create({ publicKey: pubKey as unknown as PublicKeyCredentialCreationOptions }) as PublicKeyCredential
      if (!credential) { setPasskeyMsg('Registration cancelled.'); return }

      const response = credential.response as AuthenticatorAttestationResponse
      await authApi.passkeyRegisterFinish({
        id: credential.id,
        rawId: bufferToBase64url(credential.rawId),
        type: credential.type,
        response: {
          attestationObject: bufferToBase64url(response.attestationObject),
          clientDataJSON: bufferToBase64url(response.clientDataJSON),
        },
      })
      setPasskeyMsg('Passkey registered successfully!')
      queryClient.invalidateQueries({ queryKey: ['me'] })
    } catch {
      setPasskeyMsg('Passkey registration failed.')
    }
  }

  const handleRemovePasskey = async () => {
    await authApi.passkeyRemove()
    setPasskeyMsg('Passkey removed.')
    queryClient.invalidateQueries({ queryKey: ['me'] })
  }

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Settings</h1>

      {/* Passkey Management */}
      <div className="bg-white rounded-lg shadow p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Passkey Authentication</h2>
        <p className="text-sm text-gray-500 mb-4">
          Configure a Passkey to enable password-less login as an alternative to GitHub OAuth.
        </p>
        {passkeyMsg && <div className="mb-3 p-2 bg-blue-50 text-blue-700 rounded text-sm">{passkeyMsg}</div>}
        {me?.has_passkey ? (
          <div className="flex items-center gap-4">
            <span className="text-sm text-green-600 font-medium">Passkey configured</span>
            <button onClick={handleRemovePasskey} className="text-sm text-red-500 hover:underline">Remove</button>
          </div>
        ) : (
          <button onClick={handleRegisterPasskey}
            className="px-4 py-2 bg-blue-600 text-white rounded text-sm hover:bg-blue-700">
            Register Passkey
          </button>
        )}
      </div>

      {/* Agent Profiles */}
      <div className="bg-white rounded-lg shadow p-6 mb-6">
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-lg font-semibold">Agent Profiles</h2>
          <button onClick={() => setShowForm(!showForm)} className="text-sm text-blue-600">{showForm ? 'Cancel' : '+ New'}</button>
        </div>

        {showForm && (
          <div className="mb-4 p-4 bg-gray-50 rounded space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <input value={form.provider} onChange={(e) => setForm({...form, provider: e.target.value})} placeholder="Provider (e.g. claude-code)" className="px-3 py-1.5 border rounded text-sm" />
              <input value={form.model} onChange={(e) => setForm({...form, model: e.target.value})} placeholder="Model name" className="px-3 py-1.5 border rounded text-sm" />
            </div>
            <div className="flex gap-4">
              <label className="flex items-center gap-1 text-sm"><input type="checkbox" checked={form.supports_image} onChange={(e) => setForm({...form, supports_image: e.target.checked})} /> Image support</label>
              <label className="flex items-center gap-1 text-sm"><input type="checkbox" checked={form.supports_resume} onChange={(e) => setForm({...form, supports_resume: e.target.checked})} /> Resume support</label>
            </div>
            <button onClick={() => createMutation.mutate()} disabled={!form.provider || !form.model} className="px-3 py-1.5 bg-blue-600 text-white rounded text-sm disabled:opacity-50">Create</button>
          </div>
        )}

        {models && models.length > 0 ? (
          <table className="min-w-full divide-y divide-gray-200">
            <thead>
              <tr>
                <th className="text-left text-xs font-medium text-gray-500 uppercase py-2">Provider</th>
                <th className="text-left text-xs font-medium text-gray-500 uppercase py-2">Model</th>
                <th className="text-left text-xs font-medium text-gray-500 uppercase py-2">Image</th>
                <th className="text-left text-xs font-medium text-gray-500 uppercase py-2">Resume</th>
                <th className="text-left text-xs font-medium text-gray-500 uppercase py-2"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200">
              {models.map((m: AgentProfile) => (
                <tr key={m.id}>
                  <td className="py-2 text-sm">{m.provider}</td>
                  <td className="py-2 text-sm">{m.model}</td>
                  <td className="py-2 text-sm">{m.supports_image ? 'Yes' : 'No'}</td>
                  <td className="py-2 text-sm">{m.supports_resume ? 'Yes' : 'No'}</td>
                  <td className="py-2 text-sm"><button onClick={() => deleteMutation.mutate(m.id)} className="text-red-500 text-xs hover:underline">Delete</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : !showForm && <p className="text-sm text-gray-500">No agent profiles configured.</p>}
      </div>

      <div className="bg-white rounded-lg shadow p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Git Provider</h2>
        <p className="text-sm text-gray-500">Configure GitHub App credentials in <code className="bg-gray-100 px-1 rounded">config.yaml</code>.</p>
      </div>

      <div className="bg-white rounded-lg shadow p-6">
        <h2 className="text-lg font-semibold mb-4">Comment Commands</h2>
        <p className="text-sm text-gray-500 mb-2">Supported commands in Issue/PR comments:</p>
        <ul className="text-sm space-y-1 text-gray-700">
          <li><code className="bg-gray-100 px-1 rounded">/ccmate run</code> - Start a task</li>
          <li><code className="bg-gray-100 px-1 rounded">/ccmate pause</code> - Pause the active task</li>
          <li><code className="bg-gray-100 px-1 rounded">/ccmate resume</code> - Resume a paused task</li>
          <li><code className="bg-gray-100 px-1 rounded">/ccmate retry</code> - Retry a failed task</li>
          <li><code className="bg-gray-100 px-1 rounded">/ccmate status</code> - Show task status</li>
          <li><code className="bg-gray-100 px-1 rounded">/ccmate fix-review</code> - Trigger review fix</li>
        </ul>
      </div>
    </div>
  )
}

function bufferToBase64url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let str = ''
  for (const b of bytes) str += String.fromCharCode(b)
  return btoa(str).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
}

function base64urlToBuffer(base64url: string): ArrayBuffer {
  const base64 = base64url.replace(/-/g, '+').replace(/_/g, '/')
  const padded = base64 + '='.repeat((4 - (base64.length % 4)) % 4)
  const binary = atob(padded)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return bytes.buffer
}
