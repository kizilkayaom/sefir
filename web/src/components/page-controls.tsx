import { Button } from '@/components/ui/button'

type PageControlsProps = {
  offset: number
  hasMore: boolean
  loading: boolean
  onPage: (offset: number) => void
  onRefresh: () => void
}

export function PageControls({ offset, hasMore, loading, onPage, onRefresh }: PageControlsProps) {
  return (
    <div className="my-4 flex flex-wrap items-center gap-2">
      <Button variant="outline" disabled={loading || offset === 0} onClick={() => onPage(Math.max(0, offset - 25))}>
        Previous
      </Button>
      <span className="px-2 text-sm text-muted-foreground">Page {Math.floor(offset / 25) + 1}</span>
      <Button variant="outline" disabled={loading || !hasMore} onClick={() => onPage(offset + 25)}>
        Next
      </Button>
      <Button variant="ghost" disabled={loading} onClick={onRefresh}>Refresh</Button>
    </div>
  )
}
