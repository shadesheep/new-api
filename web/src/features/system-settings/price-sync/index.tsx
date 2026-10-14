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
import { useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'

import {
  PRICE_SYNC_DEFAULT_SECTION,
  getPriceSyncSectionContent,
  getPriceSyncSectionMeta,
  type PriceSyncSectionId,
} from './section-registry'

const PRICE_SYNC_ROUTE_PATH =
  '/_authenticated/system-settings/price-sync/$section'

export function PriceSyncSettings() {
  const { t } = useTranslation()
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const params = useParams({ from: PRICE_SYNC_ROUTE_PATH as any })
  const activeSection = (params?.section ??
    PRICE_SYNC_DEFAULT_SECTION) as PriceSyncSectionId
  const sectionMeta = getPriceSyncSectionMeta(activeSection)

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t(sectionMeta.titleKey)}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 w-full flex-col gap-4'>
          {getPriceSyncSectionContent(activeSection, {})}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
