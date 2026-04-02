import { useState } from 'react'

export default function SetupPage() {
  const [token, setToken] = useState('')
  const [error, setError] = useState('')
  const [step, setStep] = useState<'token' | 'config'>('token')
  const [form, setForm] = useState({
    github_client_id: '', github_client_secret: '', github_callback_url: '',
    allowed_users: '',
    github_webhook_secret: '', github_personal_token: '',
    agent_provider: 'claude-code', agent_binary: 'claude',
    label_rules: 'ccmate:auto',
    max_concurrency: 2, task_timeout_minutes: 60,
    rp_display_name: 'ccmate', rp_id: '', rp_origin: '',
  })

  const handleTokenSubmit = async () => {
    setError('')
    // Validate token by checking if it's not empty
    if (!token.trim()) {
      setError('Please enter the setup token from the server log')
      return
    }
    setStep('config')
  }

  const handleSetup = async () => {
    setError('')

    const labelRules = form.label_rules.split('\n').filter(Boolean).map((line) => {
      const [label, mode] = line.split(':')
      return { label: label.trim(), trigger_mode: (mode || 'auto').trim() }
    })

    const res = await fetch('/api/setup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        token,
        setup: {
          github_client_id: form.github_client_id,
          github_client_secret: form.github_client_secret,
          github_callback_url: form.github_callback_url || undefined,
          allowed_users: form.allowed_users.split(',').map((s) => s.trim()).filter(Boolean),
          github_webhook_secret: form.github_webhook_secret,
          github_personal_token: form.github_personal_token,
          agent_provider: form.agent_provider,
          agent_binary: form.agent_binary,
          label_rules: labelRules,
          max_concurrency: form.max_concurrency,
          task_timeout_minutes: form.task_timeout_minutes,
          rp_display_name: form.rp_display_name,
          rp_id: form.rp_id || window.location.hostname,
          rp_origin: form.rp_origin || window.location.origin,
        },
      }),
    })

    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: 'Setup failed' }))
      setError(err.error)
      return
    }

    window.location.reload()
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50">
      <div className="max-w-2xl w-full bg-white rounded-lg shadow p-8">
        <h1 className="text-2xl font-bold text-center mb-2">ccmate Setup</h1>
        <p className="text-sm text-gray-500 text-center mb-6">First-time system configuration</p>

        {error && <div className="mb-4 p-3 bg-red-50 text-red-700 rounded text-sm">{error}</div>}

        {step === 'token' ? (
          <div className="space-y-4">
            <p className="text-sm text-gray-600">Enter the setup token from the server log output.</p>
            <input value={token} onChange={(e) => setToken(e.target.value)} placeholder="Setup token"
              className="w-full px-3 py-2 border rounded text-sm" />
            <button onClick={handleTokenSubmit} className="w-full py-2 bg-blue-600 text-white rounded text-sm font-medium hover:bg-blue-700">
              Continue
            </button>
          </div>
        ) : (
          <div className="space-y-6">
            {/* GitHub OAuth */}
            <section>
              <div className="flex items-center justify-between mb-3">
                <h2 className="text-lg font-semibold">GitHub OAuth</h2>
                <a href="https://github.com/settings/developers" target="_blank" rel="noopener noreferrer"
                  className="text-xs text-blue-600 hover:underline">Create OAuth App &rarr;</a>
              </div>
              <p className="text-xs text-gray-500 mb-3">
                Create a new OAuth App at GitHub Developer Settings. Set the <strong>Authorization callback URL</strong> to:
                <code className="ml-1 px-1 bg-gray-100 rounded">{window.location.origin}/api/auth/github/callback</code>
              </p>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs text-gray-500 mb-1">Client ID</label>
                  <input value={form.github_client_id} onChange={(e) => setForm({...form, github_client_id: e.target.value})}
                    className="w-full px-3 py-1.5 border rounded text-sm" />
                </div>
                <div>
                  <label className="block text-xs text-gray-500 mb-1">Client Secret</label>
                  <input type="password" value={form.github_client_secret} onChange={(e) => setForm({...form, github_client_secret: e.target.value})}
                    className="w-full px-3 py-1.5 border rounded text-sm" />
                </div>
              </div>
              <div className="mt-2">
                <label className="block text-xs text-gray-500 mb-1">Callback URL (leave empty for default)</label>
                <input value={form.github_callback_url} onChange={(e) => setForm({...form, github_callback_url: e.target.value})}
                  placeholder={`${window.location.origin}/api/auth/github/callback`}
                  className="w-full px-3 py-1.5 border rounded text-sm" />
              </div>
              <div className="mt-2">
                <label className="block text-xs text-gray-500 mb-1">Allowed GitHub Users (comma separated)</label>
                <input value={form.allowed_users} onChange={(e) => setForm({...form, allowed_users: e.target.value})}
                  placeholder="user1, user2" className="w-full px-3 py-1.5 border rounded text-sm" />
              </div>
            </section>

            {/* GitHub API */}
            <section>
              <div className="flex items-center justify-between mb-3">
                <h2 className="text-lg font-semibold">GitHub API</h2>
                <a href="https://github.com/settings/tokens?type=beta" target="_blank" rel="noopener noreferrer"
                  className="text-xs text-blue-600 hover:underline">Create Fine-grained Token &rarr;</a>
              </div>
              <div className="p-3 bg-amber-50 border border-amber-200 rounded text-xs text-amber-800 mb-3">
                <strong>Required permissions</strong> (Fine-grained token, select target repos):
                <ul className="mt-1 ml-4 list-disc space-y-0.5">
                  <li><strong>Contents</strong>: Read and write (clone, push, commit)</li>
                  <li><strong>Issues</strong>: Read and write (create issues, post comments)</li>
                  <li><strong>Pull requests</strong>: Read and write (create PRs, read reviews)</li>
                  <li><strong>Metadata</strong>: Read-only (list repos)</li>
                </ul>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs text-gray-500 mb-1">Personal Access Token</label>
                  <input type="password" value={form.github_personal_token} onChange={(e) => setForm({...form, github_personal_token: e.target.value})}
                    placeholder="github_pat_..." className="w-full px-3 py-1.5 border rounded text-sm" />
                </div>
                <div>
                  <label className="block text-xs text-gray-500 mb-1">Webhook Secret (for signature verification)</label>
                  <input value={form.github_webhook_secret} onChange={(e) => setForm({...form, github_webhook_secret: e.target.value})}
                    className="w-full px-3 py-1.5 border rounded text-sm" />
                </div>
              </div>
            </section>

            {/* Agent */}
            <section>
              <h2 className="text-lg font-semibold mb-3">Agent</h2>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs text-gray-500 mb-1">Provider</label>
                  <input value={form.agent_provider} onChange={(e) => setForm({...form, agent_provider: e.target.value})}
                    className="w-full px-3 py-1.5 border rounded text-sm" />
                </div>
                <div>
                  <label className="block text-xs text-gray-500 mb-1">Binary</label>
                  <input value={form.agent_binary} onChange={(e) => setForm({...form, agent_binary: e.target.value})}
                    className="w-full px-3 py-1.5 border rounded text-sm" />
                </div>
              </div>
            </section>

            {/* Limits */}
            <section>
              <h2 className="text-lg font-semibold mb-3">Limits</h2>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs text-gray-500 mb-1">Max Global Concurrency</label>
                  <input type="number" value={form.max_concurrency} onChange={(e) => setForm({...form, max_concurrency: parseInt(e.target.value) || 2})}
                    className="w-full px-3 py-1.5 border rounded text-sm" />
                </div>
                <div>
                  <label className="block text-xs text-gray-500 mb-1">Task Timeout (minutes)</label>
                  <input type="number" value={form.task_timeout_minutes} onChange={(e) => setForm({...form, task_timeout_minutes: parseInt(e.target.value) || 60})}
                    className="w-full px-3 py-1.5 border rounded text-sm" />
                </div>
              </div>
            </section>

            {/* Label Rules */}
            <section>
              <h2 className="text-lg font-semibold mb-3">Label Rules</h2>
              <label className="block text-xs text-gray-500 mb-1">One per line, format: label:mode (e.g. ccmate:auto)</label>
              <textarea value={form.label_rules} onChange={(e) => setForm({...form, label_rules: e.target.value})}
                rows={3} className="w-full px-3 py-1.5 border rounded text-sm font-mono" />
            </section>

            <button onClick={handleSetup} className="w-full py-2 bg-blue-600 text-white rounded text-sm font-medium hover:bg-blue-700">
              Complete Setup
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
