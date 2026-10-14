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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { formatPricingNumber } from '../models/pricing-format'
import { getPriceSyncSources, updatePriceSyncSources } from './api'
import { PriceSyncPreviewDialog } from './preview-dialog'
import { PriceSyncSourceDialog } from './source-dialog'
import type { PriceSyncSource } from './types'

export function PriceSyncSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogState, setDialogState] = useState<{
    open: boolean
    source: PriceSyncSource | null
  }>({ open: false, source: null })
  const [deleteTarget, setDeleteTarget] = useState<PriceSyncSource | null>(null)
  const [previewSource, setPreviewSource] = useState<PriceSyncSource | null>(
    null
  )

  const sourcesQuery = useQuery({
    queryKey: ['price-sync-sources'],
    queryFn: async () => requireServerSuccess(await getPriceSyncSources()),
  })
  const sources = useMemo(
    () => sourcesQuery.data?.data.sources ?? [],
    [sourcesQuery.data]
  )

  const saveMutation = useMutation({
    mutationFn: async (next: PriceSyncSource[]) =>
      requireServerSuccess(await updatePriceSyncSources({ sources: next })),
    onSuccess: async () => {
      toast.success(t('Sync sources saved'))
      setDialogState({ open: false, source: null })
      setDeleteTarget(null)
      await queryClient.invalidateQueries({ queryKey: ['price-sync-sources'] })
    },
    onError: (error: Error) =>
      handleServerError(error, t('Failed to save sync sources')),
  })

  const handleSave = (next: PriceSyncSource) => {
    const index = next.id
      ? sources.findIndex((item) => item.id === next.id)
      : -1
    const updated =
      index >= 0
        ? sources.map((item, itemIndex) => (itemIndex === index ? next : item))
        : [...sources, next]
    saveMutation.mutate(updated)
  }

  const handleDelete = () => {
    if (!deleteTarget) return
    saveMutation.mutate(sources.filter((item) => item.id !== deleteTarget.id))
  }

  const openAddDialog = () => setDialogState({ open: true, source: null })

  let content
  if (sourcesQuery.isLoading) {
    content = <LoadingState message={t('Loading sync sources...')} />
  } else if (sourcesQuery.isError) {
    content = (
      <ErrorState
        title={t('Failed to load sync sources')}
        description={sourcesQuery.error.message}
        onRetry={() => void sourcesQuery.refetch()}
      />
    )
  } else if (sources.length === 0) {
    content = (
      <EmptyState
        icon={RefreshCw}
        title={t('No sync sources yet')}
        description={t(
          'Add a model plaza endpoint to pull group discounts and model prices from.'
        )}
        action={
          <Button onClick={openAddDialog}>
            <Plus className='size-4' aria-hidden />
            {t('Add sync source')}
          </Button>
        }
        bordered
      />
    )
  } else {
    content = (
      <div className='flex flex-col gap-3'>
        {sources.map((source) => {
          const enabled = source.enabled !== false
          const knownGroups = source.known_groups ?? []
          return (
            <Card key={source.id}>
              <CardHeader>
                <div className='flex flex-wrap items-start justify-between gap-3'>
                  <div className='min-w-0 flex-1'>
                    <div className='flex flex-wrap items-center gap-2'>
                      <CardTitle className='text-base'>{source.name}</CardTitle>
                      {enabled ? (
                        <Badge variant='outline'>{t('Enabled')}</Badge>
                      ) : (
                        <Badge variant='secondary'>{t('Disabled')}</Badge>
                      )}
                    </div>
                    <CardDescription className='mt-1 font-mono text-xs break-all'>
                      {source.url}
                    </CardDescription>
                  </div>
                  <div className='flex shrink-0 flex-wrap items-center gap-2'>
                    <Button
                      size='sm'
                      disabled={!enabled || saveMutation.isPending}
                      onClick={() => setPreviewSource(source)}
                    >
                      <RefreshCw className='size-4' aria-hidden />
                      {t('Sync now')}
                    </Button>
                    <Button
                      size='sm'
                      variant='outline'
                      disabled={saveMutation.isPending}
                      onClick={() => setDialogState({ open: true, source })}
                    >
                      <Pencil className='size-4' aria-hidden />
                      {t('Edit')}
                    </Button>
                    <Button
                      size='sm'
                      variant='ghost'
                      disabled={saveMutation.isPending}
                      onClick={() => setDeleteTarget(source)}
                    >
                      <Trash2 className='size-4' aria-hidden />
                      {t('Delete')}
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent className='flex flex-col gap-2 text-sm'>
                <div className='flex flex-wrap items-center gap-x-6 gap-y-1'>
                  <span>
                    {t('Conversion factor')}:{' '}
                    <span className='font-mono'>
                      {formatPricingNumber(source.conversion_factor)}
                    </span>
                  </span>
                  <span>
                    {t('Markup factor')}:{' '}
                    <span className='font-mono'>
                      {formatPricingNumber(source.markup_factor)}
                    </span>
                  </span>
                </div>
                {knownGroups.length > 0 && (
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='text-muted-foreground text-xs'>
                      {t('Groups synced last time:')}
                    </span>
                    {knownGroups.map((group) => (
                      <Badge
                        key={group.remote_id}
                        variant={group.enabled ? 'outline' : 'secondary'}
                      >
                        {group.enabled
                          ? group.local_name
                          : t('{{name}} (excluded)', {
                              name: group.local_name,
                            })}
                      </Badge>
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>
          )
        })}
      </div>
    )
  }

  return (
    <div className='flex h-full min-h-0 flex-col gap-4 overflow-y-auto'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Pull group discounts and model prices from a model plaza. Preview every change before it is written.'
          )}
        </p>
        {sources.length > 0 && (
          <Button
            variant='outline'
            onClick={openAddDialog}
            disabled={saveMutation.isPending}
          >
            <Plus className='size-4' aria-hidden />
            {t('Add sync source')}
          </Button>
        )}
      </div>
      {content}
      <PriceSyncSourceDialog
        open={dialogState.open}
        onOpenChange={(open) => {
          if (!open) setDialogState({ open: false, source: null })
        }}
        source={dialogState.source}
        isSaving={saveMutation.isPending}
        onSave={handleSave}
      />
      <PriceSyncPreviewDialog
        open={previewSource != null}
        onOpenChange={(open) => {
          if (!open) setPreviewSource(null)
        }}
        source={previewSource}
      />
      <ConfirmDialog
        open={deleteTarget != null}
        onOpenChange={(open) => {
          if (!open && !saveMutation.isPending) setDeleteTarget(null)
        }}
        title={t('Delete sync source')}
        desc={t(
          'Delete sync source "{{name}}"? Prices already synced stay unchanged.',
          { name: deleteTarget?.name ?? '' }
        )}
        confirmText={t('Delete')}
        destructive
        isLoading={saveMutation.isPending}
        handleConfirm={handleDelete}
      />
    </div>
  )
}
