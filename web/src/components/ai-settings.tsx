import { useState } from 'react'
import type { FormEvent } from 'react'
import { Eye, EyeOff } from 'lucide-react'

import { AIModelPicker } from '@/components/ai-model-picker'
import { Button } from '@/components/ui/button'
import { useResource } from '@/hooks/use-resource'
import { api, errorMessage } from '@/lib/api'
import type { AIProvider, AIProviderOption, AISettings } from '@/types/api'

function ProviderForm({ provider, onSaved }: { provider: AIProviderOption, onSaved: (message: string) => void }) {
  const [key, setKey] = useState('')
  const [model, setModel] = useState(provider.model)
  const [visible, setVisible] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/settings/ai/${provider.id}`, { method: 'PUT', body: JSON.stringify({ api_key: key.trim(), model: model.trim() }) })
      setKey('')
      setVisible(false)
      onSaved(`${provider.name} settings saved.`)
    } catch (error) {
      setError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    if (busy || !window.confirm(`Remove the saved key and disable ${provider.name} for everyone? Any key set in the server environment will also be ignored by web drafting.`)) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/settings/ai/${provider.id}`, { method: 'DELETE' })
      setKey('')
      setVisible(false)
      onSaved(`${provider.name} disabled. Its saved key has been removed.`)
    } catch (error) {
      setError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save} className="space-y-3 rounded-lg border p-4" aria-label={`${provider.name} configuration`}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="font-semibold">{provider.name}</h3>
        <span className="text-sm text-muted-foreground">{provider.configured ? 'Configured' : 'Not configured'}</span>
      </div>
      <div>
        <label htmlFor={`${provider.id}-api-key`} className="block text-sm font-medium">{provider.name} API key</label>
        <div className="mt-2 flex gap-2">
          <input id={`${provider.id}-api-key`} className="field min-w-0" type={visible ? 'text' : 'password'} autoComplete="new-password" spellCheck={false} maxLength={4096} required={!provider.configured} disabled={busy} value={key} onChange={(event) => setKey(event.target.value)} placeholder={provider.configured ? 'Leave blank to keep the current key' : 'Paste your API key'} />
          <Button type="button" variant="outline" size="icon" className="shrink-0" disabled={busy} aria-label={`${visible ? 'Hide' : 'Show'} ${provider.name} API key`} onClick={() => setVisible(!visible)}>{visible ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}</Button>
        </div>
      </div>
      <AIModelPicker provider={provider} model={model} onChange={setModel} disabled={busy} />
      {provider.configured && key.trim() && <p className="text-sm text-muted-foreground">Save the changed API key before refreshing models.</p>}
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={busy || !model.trim() || (!provider.configured && !key.trim())}>{busy ? 'Saving…' : `Save ${provider.name}`}</Button>
        {provider.configured && <Button type="button" variant="outline" disabled={busy} onClick={remove}>Remove key and disable</Button>}
      </div>
    </form>
  )
}

function DefaultProviderForm({ current, providers, onSaved }: { current: string | null, providers: AIProvider[], onSaved: (message: string) => void }) {
  const [provider, setProvider] = useState(current ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await api('/api/settings/ai/default', { method: 'PUT', body: JSON.stringify({ provider }) })
      onSaved('Default AI provider saved.')
    } catch (error) {
      setError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save} className="space-y-3">
      <label className="block text-sm font-medium">Default AI provider
        <select className="field mt-2" value={provider} disabled={busy} onChange={(event) => setProvider(event.target.value)}>
          <option value="">Automatic — first configured provider</option>
          {providers.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
        </select>
      </label>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      <Button type="submit" variant="outline" disabled={busy}>{busy ? 'Saving…' : 'Save default provider'}</Button>
    </form>
  )
}

export function AISettingsPanel() {
  const ai = useResource<AISettings>('/api/settings/ai')
  const [message, setMessage] = useState('')
  function saved(message: string) {
    setMessage(message)
    ai.reload()
  }
  return (
    <section className="mt-6 space-y-4 rounded-xl border bg-card p-6">
      <h2 className="text-xl font-semibold">AI providers</h2>
      <p className="text-sm leading-6 text-muted-foreground">{ai.data?.can_manage ? 'These shared settings apply to every account on this installation. Keys are stored privately on this server and are never returned to the browser. Changes apply to the next draft.' : "Choose an available provider when generating a draft. API keys and model settings are managed by this installation's operator."}</p>
      {message && <p role="status" className="text-sm">{message}</p>}
      {ai.loading && <p role="status">Loading AI providers…</p>}
      {ai.error && <div className="space-y-3"><p role="alert" className="text-destructive">{ai.error}</p><Button variant="outline" onClick={ai.reload}>Retry AI providers</Button></div>}
      {ai.data?.can_manage ? <>
        {(ai.data.options ?? []).map((provider) => <ProviderForm key={`${provider.id}:${provider.model}:${provider.configured}`} provider={provider} onSaved={saved} />)}
        <DefaultProviderForm key={ai.data.default_provider ?? ''} current={ai.data.default_provider} providers={ai.data.providers} onSaved={saved} />
        <p className="text-sm leading-6 text-muted-foreground">Model lists are fetched using the saved key and cached for ten minutes. Discovery sends no résumé or contact data and does not generate text. A listed model may still require billing or additional access. Removing a key disables that provider for web drafting, including any key configured in the server environment.</p>
      </> : <>
        {ai.data && ai.data.providers.length === 0 && <p>No AI provider is configured. You can still write emails yourself.</p>}
        {ai.data && ai.data.providers.length > 0 && <ul className="space-y-3">
          {ai.data.providers.map((provider) => <li key={provider.id} className="rounded-lg border p-3">
            <p className="font-medium">{provider.name}{provider.id === ai.data?.default_provider && <span className="ml-2 text-xs text-muted-foreground">Default</span>}</p>
            <p className="mt-1 break-words text-sm text-muted-foreground">{provider.model}</p>
          </li>)}
        </ul>}
      </>}
    </section>
  )
}
