import { useState } from 'react'
import { Logo } from '../components/Icon'
import { Btn, Input, Label, Alert } from '../components/ui'
import Icon from '../components/Icon'

export default function LoginPage() {
  const [showPasskey, setShowPasskey] = useState(false)
  const [passkeyUser, setPasskeyUser] = useState('')
  const [error, setError] = useState('')

  const handleGitHubLogin = () => { window.location.href = '/api/auth/github/start' }

  const handlePasskeyLogin = async () => {
    setError('')
    if (!passkeyUser.trim()) { setError('Please enter your GitHub username'); return }
    try {
      const startRes = await fetch('/api/auth/passkey/login/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: passkeyUser }),
      })
      if (!startRes.ok) {
        const err = await startRes.json()
        setError(err.error || 'No passkey found for this user'); return
      }
      const options = await startRes.json()
      options.publicKey.challenge = base64urlToBuffer(options.publicKey.challenge)
      if (options.publicKey.allowCredentials) {
        for (const cred of options.publicKey.allowCredentials) cred.id = base64urlToBuffer(cred.id)
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
    <div className="login-bg">
      <div className="login-card">
        <div className="login-term-head">
          <div style={{ display: 'flex', gap: 6 }}>
            <span style={{ width: 9, height: 9, borderRadius: 999, background: '#ff5f57' }}/>
            <span style={{ width: 9, height: 9, borderRadius: 999, background: '#febc2e' }}/>
            <span style={{ width: 9, height: 9, borderRadius: 999, background: '#28c840' }}/>
          </div>
          <span style={{ marginLeft: 'auto' }}>ccmate — sign in</span>
        </div>
        <div className="login-body">
          <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', marginBottom: 26 }}>
            <Logo size={52}/>
            <div style={{ fontFamily: 'var(--font-mono)', fontSize: 20, fontWeight: 700, marginTop: 10, letterSpacing: '-0.01em', lineHeight: 1.2 }}>
              <span>ccmate</span><span className="brand-caret"/>
            </div>
            <div style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-muted)', marginTop: 6 }}>
              your coding agent's mate
            </div>
          </div>

          {error && <div style={{ marginBottom: 12 }}><Alert variant="error">{error}</Alert></div>}

          {showPasskey ? (
            <div className="stack stack-sm">
              <div className="fg">
                <Label>GitHub username</Label>
                <Input value={passkeyUser} onChange={(e) => setPasskeyUser(e.target.value)} placeholder="@tamachi" autoFocus/>
              </div>
              <Btn variant="primary" onClick={handlePasskeyLogin} disabled={!passkeyUser.trim()} style={{ width: '100%', height: 36, justifyContent: 'center' }}>
                <Icon name="key" size={13}/> Authenticate
              </Btn>
              <Btn variant="ghost" onClick={() => setShowPasskey(false)} style={{ width: '100%', justifyContent: 'center' }}>
                ← back
              </Btn>
            </div>
          ) : (
            <div className="stack stack-sm">
              <Btn variant="primary" onClick={handleGitHubLogin} style={{ width: '100%', height: 38, justifyContent: 'center' }}>
                <Icon name="github" size={14}/> Continue with GitHub
              </Btn>
              <Btn variant="ghost" onClick={() => setShowPasskey(true)} style={{ width: '100%', height: 38, justifyContent: 'center' }}>
                <Icon name="key" size={13}/> Sign in with Passkey
              </Btn>
              <div style={{ textAlign: 'center', marginTop: 16, fontFamily: 'var(--font-mono)', fontSize: 10.5, color: 'var(--fg-dim)' }}>
                $ ccmate login --provider github
              </div>
            </div>
          )}
        </div>
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
