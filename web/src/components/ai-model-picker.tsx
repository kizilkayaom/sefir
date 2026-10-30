import { useEffect, useState } from 'react'
import { Combobox } from '@base-ui/react/combobox'
import { Check, ChevronsUpDown } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { api, errorMessage } from '@/lib/api'
import type { AIModel, AIProviderOption } from '@/types/api'

export function AIModelPicker({ provider, model, onChange, disabled }: {
  provider: AIProviderOption
  model: string
  onChange: (model: string) => void
  disabled: boolean
}) {
  const [models, setModels] = useState<AIModel[] | null>(null)
  const [manual, setManual] = useState(false)
  const [revision, setRevision] = useState(0)
  const [finishedRevision, setFinishedRevision] = useState(-1)
  const [error, setError] = useState('')
  const loading = provider.configured && finishedRevision !== revision

  useEffect(() => {
    if (!provider.configured) return
    const controller = new AbortController()
    api<{ models: AIModel[] }>(`/api/settings/ai/${provider.id}/models`, {
      method: revision ? 'POST' : 'GET',
      signal: controller.signal,
    }).then((result) => {
      if (!controller.signal.aborted) setModels(result.models)
    }).catch((error) => {
      if (!controller.signal.aborted) setError(errorMessage(error))
    }).finally(() => {
      if (!controller.signal.aborted) setFinishedRevision(revision)
    })
    return () => controller.abort()
  }, [provider.id, provider.configured, revision])

  const current = models?.find((item) => item.id === model)
  const selected = current ?? { id: model, name: model }
  const options = current ? models! : [selected, ...(models ?? [])]
  const label = `${provider.name} model`
  const inputID = `${provider.id}-model`
  const modelPattern = provider.id === 'openrouter'
    ? '[A-Za-z0-9][A-Za-z0-9._\\-]*/[A-Za-z0-9][A-Za-z0-9._\\-]*(?::[A-Za-z0-9][A-Za-z0-9._\\-]*)?'
    : '[A-Za-z0-9][A-Za-z0-9._\\-]{0,127}'

  return (
    <div className="space-y-3">
      {manual || !provider.configured ? <label className="block text-sm font-medium" htmlFor={inputID}>{label}
        <input id={inputID} className="field mt-2" required maxLength={provider.id === 'openrouter' ? 256 : 128} pattern={modelPattern} spellCheck={false} disabled={disabled} value={model} onChange={(event) => onChange(event.target.value)} />
      </label> : <div>
        <label className="block text-sm font-medium" htmlFor={inputID}>{label}</label>
        <Combobox.Root items={options} value={selected} onValueChange={(item) => { if (item) onChange(item.id) }}
          isItemEqualToValue={(a, b) => a.id === b.id} itemToStringLabel={(item) => item.id}
          filter={(item, query) => `${item.name} ${item.id}`.toLowerCase().includes(query.toLowerCase())} disabled={disabled}>
          <div className="mt-2 flex gap-2">
            <Combobox.Input id={inputID} className="field min-w-0" placeholder="Search models" />
            <Combobox.Trigger className="flex shrink-0 items-center rounded-md border px-2" aria-label={`Show ${provider.name} models`}><ChevronsUpDown size={16} aria-hidden="true" /></Combobox.Trigger>
          </div>
          <Combobox.Portal>
            <Combobox.Positioner sideOffset={6} className="z-50 max-w-[calc(100vw-2rem)]">
              <Combobox.Popup className="w-[var(--anchor-width)] min-w-56 max-w-full overflow-hidden rounded-lg border bg-popover text-popover-foreground shadow-lg">
                <Combobox.Empty className="px-3 py-2 text-sm text-muted-foreground">No matching models. You can enter an ID manually.</Combobox.Empty>
                <Combobox.List className="max-h-64 overflow-y-auto p-1">
                  {(item: AIModel) => <Combobox.Item key={item.id} value={item} className="flex cursor-default items-center gap-2 rounded px-3 py-2 text-sm data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground">
                    <span className="min-w-0 flex-1 break-words">{item.name}{item.name !== item.id && <span className="block text-xs text-muted-foreground">{item.id}</span>}</span>
                    <Combobox.ItemIndicator><Check size={16} aria-hidden="true" /></Combobox.ItemIndicator>
                  </Combobox.Item>}
                </Combobox.List>
              </Combobox.Popup>
            </Combobox.Positioner>
          </Combobox.Portal>
        </Combobox.Root>
      </div>}
      {provider.configured ? <div className="flex flex-wrap gap-2">
        <Button type="button" variant="outline" disabled={disabled || loading} onClick={() => { setError(''); setRevision((value) => value + 1) }} aria-label={`Refresh ${provider.name} models`}>{loading ? 'Loading models…' : 'Refresh models'}</Button>
        <Button type="button" variant="ghost" disabled={disabled} onClick={() => setManual(!manual)}>{manual ? 'Choose from list' : 'Enter model ID manually'}</Button>
      </div> : <p className="text-sm text-muted-foreground">Save an API key to load available models.</p>}
      {loading && <p role="status" className="text-sm text-muted-foreground">Fetching available models…</p>}
      {provider.id === 'openrouter' && <p className="text-sm text-muted-foreground">Use the full OpenRouter model ID, such as openai/gpt-6-luna. Variants such as :free are supported.</p>}
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      {models?.length === 0 && !error && <p className="text-sm text-muted-foreground">No compatible models were found. You can enter a model ID manually.</p>}
      {models && !current && <p className="text-sm text-muted-foreground">The current model is not in the fetched list. Your selection is kept until you change it.</p>}
    </div>
  )
}
