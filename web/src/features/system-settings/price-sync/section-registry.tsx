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
import { createSectionRegistry } from '../utils/section-registry'
import { PriceSyncHistorySection } from './history-section'
import { PriceSyncSection } from './sync-section'

type PriceSyncSectionSettings = Record<string, unknown>

const PRICE_SYNC_SECTIONS = [
  {
    id: 'sync',
    titleKey: 'Price Sync',
    build: () => <PriceSyncSection />,
  },
  {
    id: 'history',
    titleKey: 'Sync History',
    build: () => <PriceSyncHistorySection />,
  },
] as const

export type PriceSyncSectionId = (typeof PRICE_SYNC_SECTIONS)[number]['id']

const priceSyncRegistry = createSectionRegistry<
  PriceSyncSectionId,
  PriceSyncSectionSettings
>({
  sections: PRICE_SYNC_SECTIONS,
  defaultSection: 'sync',
  basePath: '/system-settings/price-sync',
  urlStyle: 'path',
})

export const PRICE_SYNC_SECTION_IDS = priceSyncRegistry.sectionIds
export const PRICE_SYNC_DEFAULT_SECTION = priceSyncRegistry.defaultSection
export const getPriceSyncSectionNavItems = priceSyncRegistry.getSectionNavItems
export const getPriceSyncSectionContent = priceSyncRegistry.getSectionContent
export const getPriceSyncSectionMeta = priceSyncRegistry.getSectionMeta
