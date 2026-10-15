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
import { isAxiosError } from 'axios'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { invalidateModelPricing } from '@/features/model-pricing/api'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { applyPriceSync, previewPriceSync } from './api'
import {
  PriceSyncGroupTable,
  PriceSyncModelTable,
  PriceSyncSkippedList,
} from './plan-tables'
import type { PriceSyncSource } from './types'

type PriceSyncPreviewDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  source: PriceSyncSource | null
}

export function PriceSyncPreviewDialog({
  open,
  onOpenChange,
  source,
}: PriceSyncPreviewDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const sourceId = source?.id ?? ''
  const previewQuery = useQuery({
    queryKey: ['price-sync-preview', sourceId],
    queryFn: async () => requireServerSuccess(await previewPriceSync(sourceId)),
    enabled: open && sourceId !== '',
    gcTime: 0,
    retry: false,
  })
  const preview = previewQuery.data?.data
  // Backend slices may serialize as null when empty; normalize to arrays.
  const previewGroups = preview?.groups ?? []
  const previewModels = preview?.models ?? []
  const previewSkipped = preview?.skipped ?? []
  const usableGroupsAdded = preview?.usable_groups_added ?? []
  const [allowBelowCost, setAllowBelowCost] = useState(false)
  const digest = preview?.digest
  // Reset the explicit below-cost consent whenever a new plan is built.
  useEffect(() => {
    setAllowBelowCost(false)
  }, [digest, sourceId])
  const applyMutation = useMutation({
    mutationFn: async () => {
      if (!preview) throw new Error(t('Preview is not ready'))
      return requireServerSuccess(
        await applyPriceSync(sourceId, preview.digest, allowBelowCost)
      )
    },
    onSuccess: async (response) => {
      if (response.data.applied) {
        toast.success(t('Prices synced successfully'))
      } else {
        toast.info(t('No price changes were applied'))
      }
      onOpenChange(false)
      await queryClient.invalidateQueries({ queryKey: ['price-sync-sources'] })
      await queryClient.invalidateQueries({
        queryKey: ['price-sync-snapshots'],
      })
      await invalidateModelPricing(queryClient)
    },
    onError: (error: Error) => {
      handleServerError(error, t('Failed to sync prices'))
      if (isAxiosError(error) && error.response?.status === 409) {
        // A below-cost rejection keeps the plan valid — only refetch when the
        // digest went stale (no below_cost_groups marker on the response).
        const belowCost = (
          error.response.data as { below_cost_groups?: string[] } | undefined
        )?.below_cost_groups
        if (!belowCost || belowCost.length === 0) {
          void previewQuery.refetch()
        }
      }
    },
  })

  const summary = preview?.summary
  const hasChanges =
    summary != null &&
    (summary.groups_created + summary.groups_updated + summary.models_updated >
      0 ||
      usableGroupsAdded.length > 0)
  const belowCostGroups = previewGroups.filter(
    (group) => group.enabled && group.below_cost
  )
  const hasBelowCost = belowCostGroups.length > 0

  let content
  if (previewQuery.isLoading) {
    content = <LoadingState message={t('Building preview...')} />
  } else if (previewQuery.isError) {
    content = (
      <ErrorState
        title={t('Failed to fetch sync source')}
        description={previewQuery.error.message}
        onRetry={() => void previewQuery.refetch()}
      />
    )
  } else if (preview) {
    content = (
      <div className='flex flex-col gap-5'>
        {summary && (
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
            {summary.models_unchanged > 0 && (
              <Badge variant='outline'>
                {t('Models unchanged: {{count}}', {
                  count: summary.models_unchanged,
                })}
              </Badge>
            )}
            {summary.models_skipped > 0 && (
              <Badge variant='warning'>
                {t('Models skipped: {{count}}', {
                  count: summary.models_skipped,
                })}
              </Badge>
            )}
          </div>
        )}
        {!hasChanges && (
          <p className='text-muted-foreground text-sm' role='status'>
            {t('Local prices already match this source.')}
          </p>
        )}
        {hasBelowCost && (
          <div
            role='alert'
            className='border-destructive/40 bg-destructive/10 flex flex-col gap-2 rounded-md border p-3'
          >
            <p className='text-destructive text-sm font-medium'>
              {t('Some groups would be priced below cost')}
            </p>
            <p className='text-destructive/90 text-xs'>
              {t(
                'Sell price = upstream cost × markup. The following groups use a markup below 1 and would not cover the upstream cost: {{groups}}',
                { groups: belowCostGroups.map((group) => group.local_name).join(', ') }
              )}
            </p>
            <label className='flex items-center gap-2 text-sm'>
              <Checkbox
                checked={allowBelowCost}
                onCheckedChange={(checked) =>
                  setAllowBelowCost(checked === true)
                }
                aria-label={t('Apply below-cost pricing anyway')}
              />
              {t('I understand and want to apply below-cost pricing anyway')}
            </label>
          </div>
        )}
        {previewGroups.length > 0 && (
          <div className='flex flex-col gap-2'>
            <h3 className='text-sm font-medium'>{t('Group ratios')}</h3>
            <PriceSyncGroupTable groups={previewGroups} />
          </div>
        )}
        {previewModels.length > 0 && (
          <div className='flex flex-col gap-2'>
            <h3 className='text-sm font-medium'>{t('Model ratios')}</h3>
            <PriceSyncModelTable models={previewModels} />
          </div>
        )}
        {usableGroupsAdded.length > 0 && (
          <div className='flex flex-col gap-2'>
            <h3 className='text-sm font-medium'>
              {t('Groups added to user-usable list')}
            </h3>
            <div className='flex flex-wrap gap-2'>
              {usableGroupsAdded.map((group) => (
                <Badge key={group} variant='outline'>
                  {group}
                </Badge>
              ))}
            </div>
          </div>
        )}
        {previewSkipped.length > 0 && (
          <div className='flex flex-col gap-2'>
            <h3 className='text-sm font-medium'>{t('Skipped models')}</h3>
            <PriceSyncSkippedList skipped={previewSkipped} />
          </div>
        )}
      </div>
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!applyMutation.isPending) onOpenChange(next)
      }}
      title={t('Preview sync')}
      description={source?.name}
      contentClassName='sm:max-w-3xl'
      contentHeight='min(60vh, 560px)'
      bodyClassName='flex flex-col gap-5'
      footer={
        <>
          <Button
            variant='outline'
            disabled={applyMutation.isPending}
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            disabled={
              !preview ||
              !hasChanges ||
              (hasBelowCost && !allowBelowCost) ||
              applyMutation.isPending
            }
            onClick={() => applyMutation.mutate()}
          >
            {applyMutation.isPending ? t('Applying...') : t('Apply price sync')}
          </Button>
        </>
      }
    >
      {content}
    </Dialog>
  )
}
