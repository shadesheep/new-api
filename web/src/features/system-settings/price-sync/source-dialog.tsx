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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import { DraftNumberInput } from '../models/draft-number-input'
import type { PriceSyncGroupOverride, PriceSyncSource } from './types'

type PriceSyncSourceDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  source: PriceSyncSource | null
  isSaving: boolean
  onSave: (source: PriceSyncSource) => void
}

export function PriceSyncSourceDialog({
  open,
  onOpenChange,
  source,
  isSaving,
  onSave,
}: PriceSyncSourceDialogProps) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [token, setToken] = useState('')
  const [conversionFactor, setConversionFactor] = useState(1)
  const [markupFactor, setMarkupFactor] = useState(1)
  const [enabled, setEnabled] = useState(true)
  const [groupOverrides, setGroupOverrides] = useState<
    Record<string, PriceSyncGroupOverride>
  >({})
  // Raw text for per-group markup so "empty" can mean "inherit the source
  // markup" (DraftNumberInput would coerce empty to 0, which is invalid).
  const [markupDrafts, setMarkupDrafts] = useState<Record<string, string>>({})

  useEffect(() => {
    if (!open) return
    setName(source?.name ?? '')
    setUrl(source?.url ?? '')
    setToken(source?.token ?? '')
    setConversionFactor(source?.conversion_factor ?? 1)
    setMarkupFactor(source?.markup_factor ?? 1)
    setEnabled(source?.enabled ?? true)
    setGroupOverrides(source?.groups ?? {})
    const drafts: Record<string, string> = {}
    for (const [remoteId, override] of Object.entries(source?.groups ?? {})) {
      if (override.markup_factor != null) {
        drafts[remoteId] = String(override.markup_factor)
      }
    }
    setMarkupDrafts(drafts)
  }, [open, source])

  const knownGroups = source?.known_groups ?? []
  const markupDraftValues = Object.values(markupDrafts).filter(
    (draft) => draft.trim() !== ''
  )
  const markupDraftsValid = markupDraftValues.every((draft) => {
    const parsed = Number(draft)
    return Number.isFinite(parsed) && parsed > 0
  })
  const canSave =
    name.trim() !== '' && url.trim() !== '' && markupDraftsValid && !isSaving

  const updateOverride = (remoteId: number, patch: PriceSyncGroupOverride) => {
    setGroupOverrides((previous) => ({
      ...previous,
      [String(remoteId)]: {
        ...previous[String(remoteId)],
        ...patch,
      },
    }))
  }

  const handleConfirm = () => {
    const groups: Record<string, PriceSyncGroupOverride> = {}
    for (const group of knownGroups) {
      const override = groupOverrides[String(group.remote_id)] ?? {}
      const entry: PriceSyncGroupOverride = {}
      const localName = override.name?.trim() ?? ''
      if (localName !== '' && localName !== group.remote_name) {
        entry.name = localName
      }
      if (override.enabled === false) {
        entry.enabled = false
      }
      const markupDraft = markupDrafts[String(group.remote_id)]?.trim() ?? ''
      if (markupDraft !== '') {
        const parsed = Number(markupDraft)
        if (Number.isFinite(parsed) && parsed > 0) {
          entry.markup_factor = parsed
        }
      }
      if (Object.keys(entry).length > 0) {
        groups[String(group.remote_id)] = entry
      }
    }
    onSave({
      // Empty id marks a new source; the backend derives one on save.
      id: source?.id ?? '',
      name: name.trim(),
      url: url.trim(),
      token: token.trim(),
      conversion_factor: conversionFactor,
      markup_factor: markupFactor,
      enabled,
      groups,
    })
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!isSaving) onOpenChange(next)
      }}
      title={source ? t('Edit sync source') : t('Add sync source')}
      description={t(
        'Prices are pulled from a model plaza endpoint. Preview before anything is written.'
      )}
      contentClassName='sm:max-w-xl'
      bodyClassName='flex flex-col gap-5'
      footer={
        <>
          <Button
            variant='outline'
            disabled={isSaving}
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button disabled={!canSave} onClick={handleConfirm}>
            {isSaving ? t('Saving...') : t('Save')}
          </Button>
        </>
      }
    >
      <Field>
        <FieldLabel htmlFor='price-sync-source-name'>{t('Name')}</FieldLabel>
        <Input
          id='price-sync-source-name'
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder='fufei'
        />
      </Field>
      <Field>
        <FieldLabel htmlFor='price-sync-source-url'>
          {t('Model plaza URL')}
        </FieldLabel>
        <Input
          id='price-sync-source-url'
          value={url}
          onChange={(event) => setUrl(event.target.value)}
          placeholder='https://example.com/api/v1/model-plaza'
          className='font-mono text-xs'
        />
      </Field>
      <Field>
        <FieldLabel htmlFor='price-sync-source-token'>
          {t('Access token (optional)')}
        </FieldLabel>
        <Input
          id='price-sync-source-token'
          value={token}
          onChange={(event) => setToken(event.target.value)}
          placeholder={t('Bearer token sent when fetching')}
          className='font-mono text-xs'
        />
      </Field>
      <div className='grid gap-5 sm:grid-cols-2'>
        <Field>
          <FieldLabel>{t('Conversion factor')}</FieldLabel>
          <DraftNumberInput
            value={conversionFactor}
            onValueChange={setConversionFactor}
            className='h-8'
          />
          <FieldDescription>
            {t(
              'Multiplies absolute prices during conversion. Ratios between prices stay unchanged.'
            )}
          </FieldDescription>
        </Field>
        <Field>
          <FieldLabel>{t('Markup factor')}</FieldLabel>
          <DraftNumberInput
            value={markupFactor}
            onValueChange={setMarkupFactor}
            className='h-8'
          />
          <FieldDescription>
            {t(
              'Sell markup over the upstream cost floor (official price × group rate). 1 means selling at cost.'
            )}
          </FieldDescription>
        </Field>
      </div>
      <div className='flex items-center justify-between gap-3 rounded-md border p-3'>
        <div className='min-w-0'>
          <div className='text-sm font-medium'>{t('Enabled')}</div>
          <div className='text-muted-foreground text-xs'>
            {t('Disabled sources are skipped by preview and sync.')}
          </div>
        </div>
        <Switch checked={enabled} onCheckedChange={setEnabled} />
      </div>
      {knownGroups.length > 0 && (
        <div className='flex flex-col gap-3'>
          <div>
            <div className='text-sm font-medium'>{t('Group mapping')}</div>
            <div className='text-muted-foreground text-xs'>
              {t(
                'Rename or exclude upstream groups. Leave the name empty to keep the upstream name. Leave markup empty to inherit the source markup.'
              )}
            </div>
          </div>
          <div className='flex max-h-64 flex-col gap-2 overflow-y-auto'>
            {knownGroups.map((group) => {
              const override = groupOverrides[String(group.remote_id)] ?? {}
              const markupDraft =
                markupDrafts[String(group.remote_id)] ?? ''
              const markupInvalid =
                markupDraft.trim() !== '' &&
                (!Number.isFinite(Number(markupDraft)) ||
                  Number(markupDraft) <= 0)
              return (
                <div
                  key={group.remote_id}
                  className='flex items-center gap-3 rounded-md border p-3'
                >
                  <div className='min-w-0 flex-1'>
                    <div className='flex flex-wrap items-baseline gap-2'>
                      <span className='truncate text-sm font-medium'>
                        {group.remote_name}
                      </span>
                      <span className='text-muted-foreground text-xs'>
                        #{group.remote_id}
                      </span>
                    </div>
                    <Input
                      value={override.name ?? ''}
                      onChange={(event) =>
                        updateOverride(group.remote_id, {
                          name: event.target.value,
                        })
                      }
                      placeholder={group.remote_name}
                      className='mt-2 h-8'
                      aria-label={t('Local group name')}
                    />
                  </div>
                  <div className='w-24 shrink-0'>
                    <Input
                      type='number'
                      value={markupDraft}
                      onChange={(event) =>
                        setMarkupDrafts((previous) => ({
                          ...previous,
                          [String(group.remote_id)]: event.target.value,
                        }))
                      }
                      placeholder={String(markupFactor)}
                      className={`h-8${markupInvalid ? ' border-destructive' : ''}`}
                      aria-label={t('Group markup factor')}
                      title={t('Group markup factor')}
                    />
                    <div className='text-muted-foreground mt-1 text-center text-xs'>
                      {t('Markup')}
                    </div>
                  </div>
                  <Switch
                    checked={override.enabled !== false}
                    onCheckedChange={(checked) =>
                      updateOverride(group.remote_id, { enabled: checked })
                    }
                    aria-label={t('Sync this group')}
                  />
                </div>
              )
            })}
          </div>
        </div>
      )}
    </Dialog>
  )
}
