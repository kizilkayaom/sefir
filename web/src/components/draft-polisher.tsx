import { useEffect, useId, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { ChevronDown } from 'lucide-react'

import { PolishComparison } from '@/components/polish-comparison'
import { Button } from '@/components/ui/button'
import { WritingProfilePicker } from '@/components/writing-profile-picker'
import { useResource } from '@/hooks/use-resource'
import { useUnsavedChanges } from '@/hooks/use-unsaved-changes'
import { api, errorMessage } from '@/lib/api'
import type { AccountStatus, AISettings, WritingStyleProfile } from '@/types/api'
import type { Draft, PolishSuggestion } from '@/types/draft'

type DraftPolisherProps = {
  draft: Draft
  onSaved: (draft: Draft) => void
  onCancel: () => void
  onDirtyChange: (dirty: boolean, message?: string) => void
}

export function DraftPolisher({ draft, onSaved, onCancel, onDirtyChange }: DraftPolisherProps) {
  const id = useId()
  const ai = useResource<AISettings>('/api/settings/ai')
  const account = useResource<AccountStatus>('/api/settings/account')
  const [provider, setProvider] = useState('')
  const [styleOverride, setStyleOverride] = useState('')
  const [customProfile, setCustomProfile] = useState<WritingStyleProfile | null>(null)
  const [suggestion, setSuggestion] = useState<PolishSuggestion | null>(null)
  const [busy, setBusy] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const activeRequest = useRef<AbortController | null>(null)
  const styleID = styleOverride || account.data?.default_preset_id || ''
  const usingCustomProfile = styleID === 'custom'
  const providerID = provider || ai.data?.default_provider || ''
  const selectedProvider = ai.data?.providers.find((item) => item.id === providerID)
  const canPolish = !busy && !saving && !suggestion && !ai.loading && !ai.error && !!selectedProvider &&
    !account.loading && !account.error && !!account.data && !!styleID && (!usingCustomProfile || !!customProfile)
  const savingMessage = 'Leave while this suggestion is being saved? Saving may still complete after you leave.'
  const discardMessage = saving ? savingMessage : busy ? 'Cancel polishing and leave this draft?' : 'Discard this polishing suggestion?'
  useUnsavedChanges(busy || saving || suggestion !== null, discardMessage)

  useEffect(() => () => {
    activeRequest.current?.abort()
    onDirtyChange(false)
  }, [onDirtyChange])

  async function polish(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canPolish || !selectedProvider || activeRequest.current) return
    const controller = new AbortController()
    activeRequest.current = controller
    setBusy(true)
    setError('')
    onDirtyChange(true)
    try {
      const result = await api<PolishSuggestion>(`/api/drafts/${draft.draft_id}/versions/${draft.version}/polish`, {
        method: 'POST',
        signal: controller.signal,
        body: JSON.stringify({
          content_hash: draft.content_hash,
          provider: selectedProvider.id,
          ...(usingCustomProfile
            ? { profile_id: customProfile?.profile_id }
            : { preset_id: styleID }),
        }),
      })
      if (controller.signal.aborted) return
      if (result.draft_id !== draft.draft_id || result.version !== draft.version || result.source_content_hash !== draft.content_hash) {
        throw new Error('The suggestion does not match this draft. Reload the draft and try again.')
      }
      if (typeof result.subject !== 'string' || !result.subject.trim() ||
        typeof result.body !== 'string' || !result.body.trim() || typeof result.body_html !== 'string' ||
        typeof result.preset_id !== 'string' || !result.preset_id ||
        !Number.isInteger(result.preset_revision) || result.preset_revision < 1 || result.preset_revision > 2147483647) {
        throw new Error('The server returned an invalid polishing suggestion.')
      }
      setSuggestion(result)
    } catch (error) {
      if (!controller.signal.aborted) {
        setError(errorMessage(error))
        onDirtyChange(false)
      }
    } finally {
      if (!controller.signal.aborted) {
        activeRequest.current = null
        setBusy(false)
      }
    }
  }

  async function acceptSuggestion() {
    if (!suggestion || saving || activeRequest.current) return
    if (suggestion.draft_id !== draft.draft_id || suggestion.version !== draft.version || suggestion.source_content_hash !== draft.content_hash) {
      setError('The suggestion does not match this draft. Reload the draft and try again.')
      return
    }
    const controller = new AbortController()
    activeRequest.current = controller
    setSaving(true)
    setError('')
    onDirtyChange(true, savingMessage)
    try {
      const saved = await api<Draft>(`/api/drafts/${draft.draft_id}/versions/${draft.version}/edit`, {
        method: 'POST',
        signal: controller.signal,
        body: JSON.stringify({
          subject: suggestion.subject,
          body: suggestion.body,
          body_html: suggestion.body_html,
          content_hash: suggestion.source_content_hash,
          preset_id: suggestion.preset_id,
          preset_revision: suggestion.preset_revision,
        }),
      })
      if (controller.signal.aborted) return
      onDirtyChange(false)
      onSaved(saved)
    } catch (error) {
      if (!controller.signal.aborted) {
        setError(errorMessage(error))
        onDirtyChange(true)
      }
    } finally {
      if (!controller.signal.aborted) {
        activeRequest.current = null
        setSaving(false)
      }
    }
  }

  function cancel() {
    if (saving || (suggestion && activeRequest.current)) return
    if ((activeRequest.current || suggestion) && !window.confirm(discardMessage)) return
    activeRequest.current?.abort()
    activeRequest.current = null
    onDirtyChange(false)
    onCancel()
  }

  function discard() {
    if (saving || activeRequest.current) return
    setSuggestion(null)
    setError('')
    onDirtyChange(false)
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-xl font-semibold">Polish wording</h2>
          <p className="mt-1 text-sm text-muted-foreground">Choose a provider and writing style to suggest changes to this saved draft.</p>
        </div>
        <Button type="button" variant="outline" disabled={saving} onClick={cancel}>{busy ? 'Cancel polishing' : 'Back to draft'}</Button>
      </div>
      {suggestion ? (
        <>
          <PolishComparison draft={draft} suggestion={suggestion} />
          {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
          {saving && <p role="status" className="text-sm">Saving a new draft version. You will need to review and approve it before sending.</p>}
          <div className="flex flex-wrap gap-2">
            <Button type="button" disabled={saving} onClick={acceptSuggestion}>{saving ? 'Saving…' : 'Accept suggestion'}</Button>
            <Button type="button" variant="outline" disabled={saving} onClick={discard}>Discard suggestion</Button>
          </div>
        </>
      ) : (
        <form aria-label="Polish wording" onSubmit={polish} className="rounded-xl border bg-card p-6">
          <fieldset disabled={busy} className="min-w-0 space-y-5">
            {ai.loading && <p role="status">Loading AI providers…</p>}
            {ai.error && (
              <div className="space-y-2">
                <p role="alert" className="text-destructive">{ai.error}</p>
                <Button type="button" variant="outline" onClick={ai.reload}>Retry AI providers</Button>
              </div>
            )}
            {ai.data && ai.data.providers.length === 0 && (
              <p role="status" className="text-sm text-muted-foreground">AI polishing is not configured on this installation. Your saved draft is unchanged.</p>
            )}
            {ai.data && ai.data.providers.length > 0 && (
              <div>
                <label htmlFor={`${id}-provider`} className="block text-sm font-medium">AI provider</label>
                <div className="relative mt-2">
                  <select id={`${id}-provider`} className="field appearance-none pr-10" required value={selectedProvider?.id ?? ''} aria-describedby={`${id}-data-notice`} onChange={(event) => { setProvider(event.target.value); setError('') }}>
                    <option value="" disabled>Select an AI provider</option>
                    {ai.data.providers.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
                  </select>
                  <ChevronDown aria-hidden="true" className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                </div>
                {selectedProvider && <p className="mt-2 break-words text-sm text-muted-foreground">Model: {selectedProvider.model}</p>}
                <p id={`${id}-data-notice`} className="mt-2 text-sm leading-6 text-muted-foreground">
                  {selectedProvider
                    ? usingCustomProfile
                      ? `Polishing sends this saved email and your selected writing profile to ${selectedProvider.name}.`
                      : `Polishing sends this saved email to ${selectedProvider.name}.`
                    : 'Choose which provider receives this saved email and any selected writing profile.'}
                </p>
                {selectedProvider?.id === 'openrouter' && <p className="mt-2 text-sm leading-6 text-muted-foreground">OpenRouter forwards this information to the provider serving your selected model.</p>}
              </div>
            )}
            {account.loading && <p role="status">Loading writing style…</p>}
            {account.error && (
              <div className="space-y-2">
                <p role="alert" className="text-destructive">{account.error}</p>
                <Button type="button" variant="outline" onClick={account.reload}>Retry writing style</Button>
              </div>
            )}
            {account.data && (
              <div>
                <label htmlFor={`${id}-style`} className="block text-sm font-medium">Writing style</label>
                <div className="relative mt-2">
                  <select id={`${id}-style`} className="field appearance-none pr-10" required value={styleID} onChange={(event) => { setStyleOverride(event.target.value); setError('') }}>
                    <option value="prof">Concise professional</option>
                    <option value="warm_prof">Warm professional</option>
                    <option value="technical">Technical and direct</option>
                    <option value="formal">Formal</option>
                    <option value="custom">Custom writing profile</option>
                  </select>
                  <ChevronDown aria-hidden="true" className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                </div>
                {usingCustomProfile && (
                  <div className="mt-3">
                    <WritingProfilePicker selected={customProfile} disabled={busy} onChange={(profile) => { setCustomProfile(profile); setError('') }} />
                  </div>
                )}
              </div>
            )}
            {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
            {busy && <p role="status" className="text-sm">Polishing your draft. This may take a minute.</p>}
            <Button type="submit" disabled={!canPolish}>{busy ? 'Polishing…' : 'Suggest wording'}</Button>
          </fieldset>
        </form>
      )}
    </div>
  )
}
