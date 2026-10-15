/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useQuery } from '@tanstack/react-query'
import dayjs from 'dayjs'
import { ChevronDown, History } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { getPriceSyncSnapshots } from './api'
import {
  PriceSyncGroupTable,
  PriceSyncModelTable,
  PriceSyncSkippedList,
} from './plan-tables'

export function PriceSyncHistorySection() {
  const { t } = useTranslation()
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const snapshotsQuery = useQuery({
    queryKey: ['price-sync-snapshots'],
    queryFn: async () => requireServerSuccess(await getPriceSyncSnapshots()),
  })
  const snapshots = useMemo(
    () => snapshotsQuery.data?.data.snapshots ?? [],
    [snapshotsQuery.data]
  )

  if (snapshotsQuery.isLoading) {
    return <LoadingState message={t('Loading sync history...')} />
  }

  if (snapshotsQuery.isError) {
    return (
      <ErrorState
        title={t('Failed to load sync history')}
        description={snapshotsQuery.error.message}
        onRetry={() => void snapshotsQuery.refetch()}
      />
    )
  }

  if (snapshots.length === 0) {
    return (
      <EmptyState
        icon={History}
        title={t('No sync history yet')}
        description={t(
          'Every applied price sync is recorded here with the exact changes.'
        )}
        bordered
      />
    )
  }

  return (
    <div className='flex h-full min-h-0 flex-col gap-3 overflow-y-auto'>
      {snapshots.map((snapshot) => {
        const expanded = expandedId === snapshot.id
        const summary = snapshot.summary
        return (
          <Card key={snapshot.id}>
            <CardHeader>
              <button
                type='button'
                className='flex w-full items-start justify-between gap-3 text-left'
                onClick={() => setExpandedId(expanded ? null : snapshot.id)}
                aria-expanded={expanded}
              >
                <div className='min-w-0 flex-1'>
                  <div className='flex flex-wrap items-baseline gap-x-3 gap-y-1'>
                    <CardTitle className='text-base'>
                      {snapshot.source_name}
                    </CardTitle>
                    <span className='text-muted-foreground text-xs'>
                      {dayjs(snapshot.created_at * 1000).format(
                        'YYYY-MM-DD HH:mm:ss'
                      )}
                    </span>
                  </div>
                  <CardDescription className='mt-1 font-mono text-xs'>
                    {snapshot.digest.slice(0, 12)}
                  </CardDescription>
                </div>
                <ChevronDown
                  className={cn(
                    'text-muted-foreground size-4 shrink-0 transition-transform',
                    expanded && 'rotate-180'
                  )}
                  aria-hidden
                />
              </button>
            </CardHeader>
            <CardContent className='flex flex-col gap-2 text-sm'>
              <div className='flex flex-wrap items-center gap-2'>
                <Badge variant='secondary'>
                  {t('Groups created: {{count}}', {
                    count: summary.groups_created,
                  })}
                </Badge>
                <Badge variant='secondary'>
                  {t('Groups updated: {{count}}', {
                    count: summary.groups_updated,
                  })}
                </Badge>
                <Badge variant='secondary'>
                  {t('Models updated: {{count}}', {
                    count: summary.models_updated,
                  })}
                </Badge>
                {summary.models_skipped > 0 && (
                  <Badge variant='warning'>
                    {t('Models skipped: {{count}}', {
                      count: summary.models_skipped,
                    })}
                  </Badge>
                )}
              </div>
              {expanded && (
                <div className='mt-2 flex flex-col gap-4'>
                  {(snapshot.groups?.length ?? 0) > 0 && (
                    <div className='flex flex-col gap-2'>
                      <h3 className='text-sm font-medium'>
                        {t('Group ratios')}
                      </h3>
                      <PriceSyncGroupTable groups={snapshot.groups ?? []} />
                    </div>
                  )}
                  {(snapshot.models?.length ?? 0) > 0 && (
                    <div className='flex flex-col gap-2'>
                      <h3 className='text-sm font-medium'>
                        {t('Model ratios')}
                      </h3>
                      <PriceSyncModelTable models={snapshot.models ?? []} />
                    </div>
                  )}
                  {(snapshot.skipped?.length ?? 0) > 0 && (
                    <div className='flex flex-col gap-2'>
                      <h3 className='text-sm font-medium'>
                        {t('Skipped models')}
                      </h3>
                      <PriceSyncSkippedList skipped={snapshot.skipped ?? []} />
                    </div>
                  )}
                </div>
              )}
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}
