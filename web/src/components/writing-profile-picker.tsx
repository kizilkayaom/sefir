import { useId, useState } from 'react'
import { ChevronDown } from 'lucide-react'

import { PageControls } from '@/components/page-controls'
import { Button } from '@/components/ui/button'
import { useResource } from '@/hooks/use-resource'
import type { Page, WritingStyleProfile } from '@/types/api'

type WritingProfilePickerProps = {
  selected: WritingStyleProfile | null
  onChange: (profile: WritingStyleProfile | null) => void
  disabled: boolean
}

export function WritingProfilePicker({ selected, onChange, disabled }: WritingProfilePickerProps) {
  const selectID = useId()
  const [offset, setOffset] = useState(0)
  const profiles = useResource<Page<WritingStyleProfile>>(
    `/api/settings/writing-styles?limit=25&offset=${offset}`,
  )
  const choices = [...(profiles.data?.items ?? [])]
  if (selected && !choices.some((profile) => profile.profile_id === selected.profile_id)) {
    choices.unshift(selected)
  }

  return (
    <div className="space-y-3">
      <label htmlFor={selectID} className="block text-sm font-medium">Custom writing profile</label>
      <div className="relative">
        <select
          id={selectID}
          className="field appearance-none pr-10"
          required
          disabled={disabled || profiles.loading || !!profiles.error}
          value={selected?.profile_id ?? ''}
          onChange={(event) => onChange(choices.find((profile) => profile.profile_id === event.target.value) ?? null)}
        >
          <option value="">Select a custom writing profile</option>
          {choices.map((profile) => (
            <option key={profile.profile_id} value={profile.profile_id}>
              {profile.name} — {profile.language}
            </option>
          ))}
        </select>
        <ChevronDown aria-hidden="true" className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
      </div>
      {profiles.loading && <p role="status">Loading writing profiles…</p>}
      {profiles.error && (
        <div role="alert" className="space-y-2">
          <p className="text-destructive">{profiles.error}</p>
          <Button type="button" variant="outline" disabled={disabled || profiles.loading} onClick={profiles.reload}>
            Retry writing profiles
          </Button>
        </div>
      )}
      {profiles.data?.items.length === 0 && (
        <p role="status" className="text-sm text-muted-foreground">
          {offset === 0 ? 'No custom writing profiles yet.' : 'No custom writing profiles on this page.'}
        </p>
      )}
      <PageControls
        offset={offset}
        hasMore={profiles.data?.has_more ?? false}
        loading={profiles.loading || disabled}
        onPage={setOffset}
        onRefresh={profiles.reload}
      />
    </div>
  )
}
