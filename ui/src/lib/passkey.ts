// Passkeys in the browser: turn the server's WebAuthn options (JSON, with
// base64url binary fields) into what navigator.credentials expects, and the
// device's answer back into JSON. Uses the browser's own converters where
// they exist (Safari 18.4+, Chrome 129+, Firefox 119+).

import { api } from './api'

export function passkeysSupported(): boolean {
  return typeof window !== 'undefined' && typeof window.PublicKeyCredential === 'function' && !!navigator.credentials
}

export function b64urlToBytes(s: string): ArrayBuffer {
  const b = atob(s.replace(/-/g, '+').replace(/_/g, '/') + '==='.slice((s.length + 3) % 4))
  const out = new Uint8Array(b.length)
  for (let i = 0; i < b.length; i++) out[i] = b.charCodeAt(i)
  return out.buffer
}

export function bytesToB64url(buf: ArrayBuffer): string {
  let s = ''
  for (const c of new Uint8Array(buf)) s += String.fromCharCode(c)
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

type Json = Record<string, unknown>
type PKC = typeof PublicKeyCredential & {
  parseCreationOptionsFromJSON?: (o: Json) => PublicKeyCredentialCreationOptions
  parseRequestOptionsFromJSON?: (o: Json) => PublicKeyCredentialRequestOptions
}

export function creationOptions(o: Json): PublicKeyCredentialCreationOptions {
  const P = window.PublicKeyCredential as PKC
  if (P.parseCreationOptionsFromJSON) return P.parseCreationOptionsFromJSON(o)
  const user = o.user as Json
  return {
    ...(o as unknown as PublicKeyCredentialCreationOptions),
    challenge: b64urlToBytes(o.challenge as string),
    user: { ...(user as unknown as PublicKeyCredentialUserEntity), id: b64urlToBytes(user.id as string) },
    excludeCredentials: ((o.excludeCredentials as Json[]) ?? []).map((c) => ({
      ...(c as unknown as PublicKeyCredentialDescriptor),
      id: b64urlToBytes(c.id as string),
    })),
  }
}

export function requestOptions(o: Json): PublicKeyCredentialRequestOptions {
  const P = window.PublicKeyCredential as PKC
  if (P.parseRequestOptionsFromJSON) return P.parseRequestOptionsFromJSON(o)
  return {
    ...(o as unknown as PublicKeyCredentialRequestOptions),
    challenge: b64urlToBytes(o.challenge as string),
    allowCredentials: ((o.allowCredentials as Json[]) ?? []).map((c) => ({
      ...(c as unknown as PublicKeyCredentialDescriptor),
      id: b64urlToBytes(c.id as string),
    })),
  }
}

/** The device's answer as JSON for the server. */
export function credentialJSON(c: PublicKeyCredential): Json {
  const withJSON = c as unknown as { toJSON?: () => Json }
  if (typeof withJSON.toJSON === 'function') return withJSON.toJSON()
  const r = c.response as AuthenticatorAttestationResponse & AuthenticatorAssertionResponse
  const response: Json = { clientDataJSON: bytesToB64url(r.clientDataJSON) }
  if ('attestationObject' in r && r.attestationObject) {
    response.attestationObject = bytesToB64url(r.attestationObject)
    response.transports = r.getTransports?.() ?? []
  } else {
    response.authenticatorData = bytesToB64url(r.authenticatorData)
    response.signature = bytesToB64url(r.signature)
    if (r.userHandle) response.userHandle = bytesToB64url(r.userHandle)
  }
  return {
    id: c.id,
    rawId: bytesToB64url(c.rawId),
    type: c.type,
    response,
    clientExtensionResults: c.getClientExtensionResults?.() ?? {},
    authenticatorAttachment: c.authenticatorAttachment ?? undefined,
  }
}

/** Adds a passkey for the signed-in reader on this device. */
export async function addPasskey(name: string): Promise<void> {
  const { publicKey } = await api.passkeyRegisterBegin()
  const cred = (await navigator.credentials.create({ publicKey: creationOptions(publicKey) })) as PublicKeyCredential | null
  if (!cred) throw new Error('No passkey was created.')
  await api.passkeyRegisterFinish(credentialJSON(cred), name)
}

/**
 * Signs in with a passkey. With mediation 'conditional' the browser offers
 * passkeys in the username field's autofill instead of a dialog; abort the
 * signal to stop waiting (for example, when the reader types a password).
 */
export async function signInWithPasskey(mediation?: CredentialMediationRequirement, signal?: AbortSignal): Promise<string> {
  const { publicKey } = await api.passkeyLoginBegin()
  const cred = (await navigator.credentials.get({ publicKey: requestOptions(publicKey), mediation, signal })) as PublicKeyCredential | null
  if (!cred) throw new Error('No passkey was chosen.')
  return (await api.passkeyLoginFinish(credentialJSON(cred))).username
}

/** Whether this browser can offer passkeys in autofill. */
export async function conditionalAvailable(): Promise<boolean> {
  const P = window.PublicKeyCredential as PKC & { isConditionalMediationAvailable?: () => Promise<boolean> }
  return passkeysSupported() && !!P.isConditionalMediationAvailable && (await P.isConditionalMediationAvailable().catch(() => false))
}
