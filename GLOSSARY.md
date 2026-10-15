# new-api

Domain language for this API gateway: quota billing, model pricing, and the upstream pricing features.

## Pricing Sync

**Model Plaza (模型广场)**:
A site's public catalog of its selling groups and their per-model prices.
_Avoid_: Models list, pricing API

**Price Sync Source (价格同步来源)**:
An admin-configured Model Plaza address together with its optional credential and its conversion/markup factors; the unit of one sync run.
_Avoid_: Channel, upstream

**Selling Price (卖出价)**:
The per-token price a source site charges its own customers for a model; the price Price Sync writes into this site's price table.
_Avoid_: Discounted price (that is Selling Price × Group Discount)

**Official Price (官方价)**:
The official base price a source site records for a model; used for comparison, not written by itself.

**Group Discount (分组折扣)**:
A source group's whole-group price factor, expressed as 元/刀 — CNY charged per USD of Official Price (0.45/刀 ≈ 5折, 1.03/刀 ≈ 加价3%); it applies uniformly to every model in the group.
_Avoid_: Discount rate, multiplier

**Group Ratio (分组倍率)**:
This site's per-group scalar applied to every model's price for users of that group; where Group Discount lands after conversion.
_Avoid_: Group price

**Conversion Factor (折算系数)**:
The per-source factor that aligns a source's Group Discount unit (元/刀) with this site's Group Ratio convention. Default 1.
_Avoid_: Exchange rate

**Markup Factor (加价系数)**:
The per-source factor applied on the Group Ratio side to add margin above the source's prices. Default 1 = no markup.
_Avoid_: Margin

**Sync Snapshot (同步快照)**:
The fetched source data and computed differences of one sync run, kept for comparison and audit.
_Avoid_: Log, history entry

**Price Sync (价格同步)**:
The source-driven standalone feature: one click syncs a Price Sync Source's Group Discounts and Selling Prices into this site's pricing.
_Avoid_: Upstream Price Sync

**Upstream Price Sync (上游价格同步)**:
The existing per-model comparison workflow: fetch several upstreams, diff against current prices, apply per model. Distinct from Price Sync.
_Avoid_: Price Sync
