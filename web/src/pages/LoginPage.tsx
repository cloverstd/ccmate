import { useState } from 'react'

// WebAuthn helpers
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

export default function LoginPage() {
  const [bootstrapToken, setBootstrapToken] = useState('')
  const [isRegistering, setIsRegistering] = useState(false)
  const [error, setError] = useState('')

  const handleLogin = async () => {
    setError('')
    try {
      const startRes = await fetch('/api/auth/passkey/login/start', { method: 'POST' })
      if (!startRes.ok) {
        setIsRegistering(true)
        return
      }

      const options = await startRes.json()

      // Convert base64url challenge and allowCredentials IDs to ArrayBuffers
      options.publicKey.challenge = base64urlToBuffer(options.publicKey.challenge)
      if (options.publicKey.allowCredentials) {
        for (const cred of options.publicKey.allowCredentials) {
          cred.id = base64urlToBuffer(cred.id)
        }
      }

      const credential = await navigator.credentials.get(options) as PublicKeyCredential
      if (!credential) {
        setError('Passkey authentication cancelled.')
        return
      }

      const response = credential.response as AuthenticatorAssertionResponse
      const finishRes = await fetch('/api/auth/passkey/login/finish', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
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

      if (!finishRes.ok) {
        setError('Login verification failed.')
        return
      }

      window.location.reload()
    } catch (e) {
      setError('Login failed. Passkey may not be supported in this browser.')
    }
  }

  const handleRegister = async () => {
    setError('')
    try {
      const startRes = await fetch('/api/auth/passkey/register/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ bootstrap_token: bootstrapToken }),
      })
      if (!startRes.ok) {
        setError('Invalid bootstrap token')
        return
      }

      const options = await startRes.json()

      // Convert base64url fields to ArrayBuffers for WebAuthn API
      options.publicKey.challenge = base64urlToBuffer(options.publicKey.challenge)
      options.publicKey.user.id = base64urlToBuffer(options.publicKey.user.id)
      if (options.publicKey.excludeCredentials) {
        for (const cred of options.publicKey.excludeCredentials) {
          cred.id = base64urlToBuffer(cred.id)
        }
      }

      const credential = await navigator.credentials.create(options) as PublicKeyCredential
      if (!credential) {
        setError('Passkey registration cancelled.')
        return
      }

      const response = credential.response as AuthenticatorAttestationResponse
      const finishRes = await fetch('/api/auth/passkey/register/finish', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          id: credential.id,
          rawId: bufferToBase64url(credential.rawId),
          type: credential.type,
          response: {
            attestationObject: bufferToBase64url(response.attestationObject),
            clientDataJSON: bufferToBase64url(response.clientDataJSON),
          },
        }),
      })

      if (!finishRes.ok) {
        setError('Registration verification failed.')
        return
      }

      window.location.reload()
    } catch (e) {
      setError('Registration failed. Passkey may not be supported in this browser.')
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50">
      <div className="max-w-md w-full bg-white rounded-lg shadow p-8">
        <h1 className="text-2xl font-bold text-center mb-6">ccmate</h1>

        {error && (
          <div className="mb-4 p-3 bg-red-50 text-red-700 rounded text-sm">
            {error}
          </div>
        )}

        {isRegistering ? (
          <>
            <p className="text-sm text-gray-600 mb-4">
              Enter the bootstrap token from the server log to register as admin.
            </p>
            <input
              type="text"
              value={bootstrapToken}
              onChange={(e) => setBootstrapToken(e.target.value)}
              placeholder="Bootstrap token"
              className="w-full px-3 py-2 border border-gray-300 rounded mb-4 text-sm"
            />
            <button
              onClick={handleRegister}
              className="w-full py-2 bg-blue-600 text-white rounded hover:bg-blue-700 text-sm font-medium"
            >
              Register with Passkey
            </button>
            <button
              onClick={() => setIsRegistering(false)}
              className="w-full mt-2 py-2 text-gray-600 text-sm"
            >
              Back to Login
            </button>
          </>
        ) : (
          <>
            <button
              onClick={handleLogin}
              className="w-full py-2 bg-blue-600 text-white rounded hover:bg-blue-700 text-sm font-medium"
            >
              Login with Passkey
            </button>
            <button
              onClick={() => setIsRegistering(true)}
              className="w-full mt-2 py-2 text-gray-600 text-sm"
            >
              First time? Register
            </button>
          </>
        )}
      </div>
    </div>
  )
}
