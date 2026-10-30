import { Menu } from '@base-ui/react/menu'
import { ChevronDown, LogOut, Settings, UserRound } from 'lucide-react'

export function AccountMenu({ owner, busy, onLogout }: { owner: string; busy: boolean; onLogout: () => void }) {
  const itemClass = 'flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 text-sm outline-none data-highlighted:bg-accent'
  return (
    <Menu.Root>
      <Menu.Trigger aria-label={`Account: ${owner}`} className="flex min-h-10 items-center gap-2 rounded-lg px-3 text-sm font-medium outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring">
        <UserRound aria-hidden="true" className="size-4" />
        <span className="max-w-40 truncate">{owner}</span>
        <ChevronDown aria-hidden="true" className="size-4 text-muted-foreground" />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner sideOffset={8} align="end" className="z-50">
          <Menu.Popup className="min-w-48 rounded-xl border bg-popover p-1.5 text-popover-foreground shadow-lg outline-none">
            <Menu.LinkItem href="#settings" closeOnClick className={itemClass}><Settings aria-hidden="true" className="size-4" />Settings</Menu.LinkItem>
            <Menu.Separator className="my-1 border-t" />
            <Menu.Item className={itemClass} data-discard-email disabled={busy} onClick={onLogout}>
              <LogOut aria-hidden="true" className="size-4" />{busy ? 'Signing out…' : 'Sign out'}
            </Menu.Item>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  )
}
