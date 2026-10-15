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
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Badge } from '@/components/ui/badge'

import {
  formatPricingNumber,
  formatPricingNumber2dp,
} from '../models/pricing-format'
import type {
  PriceSyncGroupPlan,
  PriceSyncModelPlan,
  PriceSyncSkippedModel,
} from './types'

export function priceSyncFieldLabel(t: TFunction, field: string): string {
  switch (field) {
    case 'model_ratio':
      return t('Model ratio')
    case 'completion_ratio':
      return t('Completion ratio')
    case 'cache_ratio':
      return t('Cache ratio')
    case 'create_cache_ratio':
      return t('Cache write ratio')
    case 'image_ratio':
      return t('Image ratio')
    case 'model_price':
      return t('Model price')
    default:
      return field
  }
}

export function priceSyncSkipReasonLabel(t: TFunction, reason: string): string {
  switch (reason) {
    case 'empty_pricing':
      return t('No prices published upstream')
    case 'zero_input_price':
      return t('Upstream input price is zero')
    case 'invalid_pricing':
      return t('Upstream prices are invalid')
    case 'price_conflict':
      return t('Conflicting prices across groups')
    case 'tiered_expr':
      return t('Model uses tiered pricing')
    case 'unsupported_pricing':
      return t('Unsupported upstream pricing mode')
    default:
      return reason
  }
}

export function PriceSyncGroupTable({
  groups,
}: {
  groups: PriceSyncGroupPlan[]
}) {
  const { t } = useTranslation()
  const columns: StaticDataTableColumn<PriceSyncGroupPlan>[] = [
    {
      id: 'name',
      header: t('Group'),
      cell: (row) => (
        <div className='flex flex-col gap-1'>
          <div className='flex flex-wrap items-center gap-2'>
            <span className='font-medium'>{row.local_name}</span>
            {row.created && <Badge variant='secondary'>{t('New')}</Badge>}
            {!row.created && row.changed && (
              <Badge variant='outline'>{t('Changed')}</Badge>
            )}
            {row.enabled && row.below_cost && (
              <Badge variant='destructive'>{t('Below cost')}</Badge>
            )}
          </div>
          <span className='text-muted-foreground text-xs'>
            {row.remote_name} · #{row.remote_id}
          </span>
        </div>
      ),
    },
    {
      id: 'cost_rate',
      header: t('Cost rate'),
      cell: (row) => (
        <span
          className='font-mono text-xs'
          title={t('Upstream cost in CNY per USD of the official price')}
        >
          {formatPricingNumber(row.cost_rate)}
        </span>
      ),
    },
    {
      id: 'markup',
      header: t('Markup'),
      cell: (row) => (
        <span
          className={`font-mono text-xs${row.enabled && row.below_cost ? ' text-destructive' : ''}`}
        >
          ×{formatPricingNumber(row.markup)}
        </span>
      ),
    },
    {
      id: 'ratio',
      header: t('Group ratio'),
      cell: (row) => (
        <span className='font-mono text-xs'>
          {row.old_ratio == null
            ? t('Not set')
            : formatPricingNumber(row.old_ratio)}
          {' → '}
          {formatPricingNumber(row.new_ratio)}
        </span>
      ),
    },
    {
      id: 'status',
      header: t('Status'),
      cell: (row) =>
        row.enabled ? (
          <Badge variant='outline'>{t('Enabled')}</Badge>
        ) : (
          <Badge variant='secondary'>{t('Disabled')}</Badge>
        ),
    },
  ]
  return <StaticDataTable data={groups} columns={columns} />
}

export function PriceSyncModelTable({
  models,
}: {
  models: PriceSyncModelPlan[]
}) {
  const { t } = useTranslation()
  const columns: StaticDataTableColumn<PriceSyncModelPlan>[] = [
    {
      id: 'model',
      header: t('Model'),
      cell: (row) => (
        <div className='flex flex-col gap-1'>
          <span className='font-medium'>{row.name}</span>
          {(row.cache_1h_derived || row.price_basis_mismatch) && (
            <div className='flex flex-wrap gap-1'>
              {row.cache_1h_derived && (
                <Badge
                  variant='outline'
                  title={t(
                    'The 1h cache write price is derived from the 5m price (×1.6), not synced from upstream.'
                  )}
                >
                  {t('1h cache write derived')}
                </Badge>
              )}
              {row.price_basis_mismatch && (
                <Badge
                  variant='warning'
                  title={t(
                    "This model does not share its group's price basis, so the group ratio may not fit it. Review it manually."
                  )}
                >
                  {t('Price basis mismatch')}
                </Badge>
              )}
            </div>
          )}
        </div>
      ),
    },
    {
      id: 'base_price',
      header: t('Base price'),
      cell: (row) =>
        row.base_price_usd_per_1m == null ? (
          <span className='text-muted-foreground text-xs'>{t('Not set')}</span>
        ) : (
          <span
            className='font-mono text-xs'
            title={t('Official input price per 1M tokens')}
          >
            ${formatPricingNumber2dp(row.base_price_usd_per_1m)} / 1M
          </span>
        ),
    },
    {
      id: 'changes',
      header: t('Changes'),
      cell: (row) => (
        <div className='flex flex-col gap-1'>
          {Object.entries(row.fields).map(([field, change]) => (
            <span key={field} className='font-mono text-xs'>
              <span className='text-muted-foreground'>
                {priceSyncFieldLabel(t, field)}:{' '}
              </span>
              {change.old == null
                ? t('Not set')
                : formatPricingNumber(change.old)}
              {' → '}
              {formatPricingNumber(change.new)}
            </span>
          ))}
        </div>
      ),
    },
  ]
  return <StaticDataTable data={models} columns={columns} />
}

export function PriceSyncSkippedList({
  skipped,
}: {
  skipped: PriceSyncSkippedModel[]
}) {
  const { t } = useTranslation()
  return (
    <ul className='flex flex-col gap-1'>
      {skipped.map((item) => (
        <li
          key={item.name}
          className='flex flex-wrap items-center justify-between gap-2 text-sm'
        >
          <span className='font-mono text-xs'>{item.name}</span>
          <span className='text-muted-foreground text-xs'>
            {priceSyncSkipReasonLabel(t, item.reason)}
          </span>
        </li>
      ))}
    </ul>
  )
}
