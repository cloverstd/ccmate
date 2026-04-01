import { useState } from 'react'

export default function LoginPage() {
  const [showPasskey, setShowPasskey] = useState(false)
  const [passkeyUser, setPasskeyUser] = useState('')
  const [error, setError] = useState('')

  const handleGitHubLogin = () => {
    window.location.href = '/api/auth/github/start'
  }

  const handlePasskeyLogin = async () => {
    setError('')
    if (!passkeyUser.trim()) {
      setError('Please enter your GitHub username')
      return
    }

    try {
      const startRes = await fetch('/api/auth/passkey/login/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: passkeyUser }),
      })
      if (!startRes.ok) {
        const err = await startRes.json()
        setError(err.error || 'No passkey found for this user')
        return
      }

      const options = await startRes.json()
      options.publicKey.challenge = base64urlToBuffer(options.publicKey.challenge)
      if (options.publicKey.allowCredentials) {
        for (const cred of options.publicKey.allowCredentials) {
          cred.id = base64urlToBuffer(cred.id)
        }
      }

      const credential = await navigator.credentials.get(options) as PublicKeyCredential
      if (!credential) { setError('Passkey cancelled.'); return }

      const response = credential.response as AuthenticatorAssertionResponse
      const finishRes = await fetch('/api/auth/passkey/login/finish', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username: passkeyUser,
          id: credential.id,
          rawId: bufferToBase64url(credential.rawId),
          type: credential.type,
          response: {
            authenticatorData: bufferToBase64url(response.authenticatorData),
            clientDataJSON: bufferToBase64url(response.clientDataJSON),
            signature: bufferToBase64url(response.signature),
            userHandle: response.userHandle ? bufferToBase64url(response.userHandle) : '',
          },
        }),
      })

      if (!finishRes.ok) { setError('Passkey verification failed.'); return }
      window.location.reload()
    } catch {
      setError('Passkey login failed.')
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50">
      <div className="max-w-md w-full bg-white rounded-lg shadow p-8">
        <h1 className="text-2xl font-bold text-center mb-6">ccmate</h1>

        {error && <div className="mb-4 p-3 bg-red-50 text-red-700 rounded text-sm">{error}</div>}

        {showPasskey ? (
          <>
            <input value={passkeyUser} onChange={(e) => setPasskeyUser(e.target.value)}
              placeholder="GitHub username" className="w-full px-3 py-2 border border-gray-300 rounded mb-4 text-sm" />
            <button onClick={handlePasskeyLogin}
              className="w-full py-2 bg-gray-800 text-white rounded hover:bg-gray-900 text-sm font-medium">
              Login with Passkey
            </button>
            <button onClick={() => setShowPasskey(false)} className="w-full mt-2 py-2 text-gray-600 text-sm">
              Back
            </button>
          </>
        ) : (
          <>
            <button onClick={handleGitHubLogin}
              className="w-full py-2 bg-gray-800 text-white rounded hover:bg-gray-900 text-sm font-medium flex items-center justify-center gap-2">
              <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 24 24"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/></svg>
              Login with GitHub
            </button>
            <button onClick={() => setShowPasskey(true)}
              className="w-full mt-2 py-2 text-gray-600 text-sm hover:text-gray-800">
              Login with Passkey
            </button>
          </>
        )}
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
