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
export type PriceSyncGroupOverride = {
  name?: string
  enabled?: boolean
}

export type PriceSyncKnownGroup = {
  remote_id: number
  remote_name: string
  local_name: string
  enabled: boolean
}

export type PriceSyncSource = {
  id: string
  name: string
  url: string
  token?: string
  conversion_factor: number
  markup_factor: number
  enabled?: boolean
  groups?: Record<string, PriceSyncGroupOverride>
  known_groups?: PriceSyncKnownGroup[]
}

export type PriceSyncGroupPlan = {
  remote_id: number
  remote_name: string
  description?: string
  local_name: string
  enabled: boolean
  rate_multiplier: number
  old_ratio: number | null
  new_ratio: number
  changed: boolean
  created: boolean
}

export type PriceSyncFieldChange = {
  old: number | null
  new: number
}

export type PriceSyncModelPlan = {
  name: string
  fields: Record<string, PriceSyncFieldChange>
}

export type PriceSyncSkippedModel = {
  name: string
  reason: string
}

export type PriceSyncSummary = {
  groups_created: number
  groups_updated: number
  models_updated: number
  models_unchanged: number
  models_skipped: number
}

export type PriceSyncPreviewData = {
  source: PriceSyncSource
  digest: string
  groups: PriceSyncGroupPlan[]
  models: PriceSyncModelPlan[]
  skipped: PriceSyncSkippedModel[]
  summary: PriceSyncSummary
  usable_groups_added: string[]
}

export type PriceSyncSnapshot = {
  id: string
  created_at: number
  source_id: string
  source_name: string
  digest: string
  summary: PriceSyncSummary
  groups: PriceSyncGroupPlan[]
  models: PriceSyncModelPlan[]
  skipped: PriceSyncSkippedModel[]
}

export type PriceSyncSourcesResponse = {
  success: boolean
  message: string
  data: {
    sources: PriceSyncSource[]
  }
}

export type PriceSyncUpdateSourcesRequest = {
  sources: PriceSyncSource[]
}

export type PriceSyncPreviewResponse = {
  success: boolean
  message: string
  data: PriceSyncPreviewData
}

export type PriceSyncApplyResponse = {
  success: boolean
  message: string
  data: {
    applied: boolean
    snapshot_id: string
    summary: PriceSyncSummary
  }
}

export type PriceSyncSnapshotsResponse = {
  success: boolean
  message: string
  data: {
    snapshots: PriceSyncSnapshot[]
  }
}
