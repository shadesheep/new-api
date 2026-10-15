package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func syncTestFloatPtr(value float64) *float64 {
	return &value
}

func syncTestPricing(input, output, cacheRead, cacheWrite float64) priceSyncPlazaPricing {
	return priceSyncPlazaPricing{
		BillingMode:     "token",
		InputPrice:      syncTestFloatPtr(input),
		OutputPrice:     syncTestFloatPtr(output),
		CacheReadPrice:  syncTestFloatPtr(cacheRead),
		CacheWritePrice: syncTestFloatPtr(cacheWrite),
	}
}

func syncTestPricingSlice() priceSyncPlazaPricing {
	return syncTestPricing(2e-5, 1e-4, 2e-6, 2e-5)
}

func TestBuildPriceSyncPlanConversion(t *testing.T) {
	previousConfig := config.GlobalConfig.ExportAllConfigs()
	t.Cleanup(func() {
		config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{
			"billing_mode": previousConfig["billing_setting.billing_mode"],
			"billing_expr": previousConfig["billing_setting.billing_expr"],
		})
	})
	config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{
		"billing_mode": `{"sync-test-tiered":"tiered_expr"}`,
	})

	source := priceSyncSource{
		ID:               "test",
		Name:             "test",
		URL:              "http://example.invalid/model-plaza",
		ConversionFactor: 1,
		MarkupFactor:     1,
	}
	groups := []priceSyncPlazaGroup{
		{ID: 31, Name: "sync-test-a", Description: "Group A", RateMultiplier: 0.45, Models: []priceSyncPlazaModel{
			{Name: "sync-test-k3", Pricing: syncTestPricingSlice()},
			{Name: "sync-test-empty", Pricing: priceSyncPlazaPricing{BillingMode: "token"}},
			{Name: "sync-test-zerocache", Pricing: priceSyncPlazaPricing{
				BillingMode:     "token",
				InputPrice:      syncTestFloatPtr(1e-5),
				OutputPrice:     syncTestFloatPtr(5e-5),
				CacheReadPrice:  syncTestFloatPtr(1e-6),
				CacheWritePrice: syncTestFloatPtr(0),
			}},
			{Name: "sync-test-perreq", Pricing: priceSyncPlazaPricing{PerRequestPrice: syncTestFloatPtr(0.5)}},
			{Name: "sync-test-conflict", Pricing: syncTestPricing(1e-5, 5e-5, 0, 0)},
			{Name: "sync-test-tiered", Pricing: syncTestPricing(2e-5, 1e-4, 0, 0)},
		}},
		{ID: 5, Name: "sync-test-b-remote", Description: "Group B", RateMultiplier: 1.75, Models: []priceSyncPlazaModel{
			{Name: "sync-test-k3", Pricing: syncTestPricingSlice()},
			{Name: "sync-test-conflict", Pricing: syncTestPricing(2e-5, 1e-4, 0, 0)},
			{Name: "sync-test-b-only", Pricing: syncTestPricing(1e-5, 1e-5, 0, 0)},
		}},
	}

	plan, err := buildPriceSyncPlan(source, groups)
	require.NoError(t, err)

	require.Len(t, plan.Groups, 2)
	byID := map[int]priceSyncGroupPlan{}
	for _, group := range plan.Groups {
		byID[group.RemoteID] = group
	}
	groupA := byID[31]
	assert.Equal(t, "sync-test-a", groupA.LocalName)
	assert.True(t, groupA.Created)
	assert.True(t, groupA.Changed)
	assert.Nil(t, groupA.OldRatio)
	assert.InDelta(t, 0.45, groupA.NewRatio, 1e-9)
	groupB := byID[5]
	assert.Equal(t, "sync-test-b-remote", groupB.LocalName)
	assert.True(t, groupB.Enabled)
	assert.InDelta(t, 1.75, groupB.NewRatio, 1e-9)
	assert.Equal(t, 2, plan.Summary.GroupsCreated)
	assert.Equal(t, 0, plan.Summary.GroupsUpdated)

	models := map[string]priceSyncModelPlan{}
	for _, model := range plan.Models {
		models[model.Name] = model
	}
	k3 := models["sync-test-k3"]
	require.Contains(t, k3.Fields, "model_ratio")
	assert.InDelta(t, 10, k3.Fields["model_ratio"].New, 1e-6)
	assert.Nil(t, k3.Fields["model_ratio"].Old)
	assert.InDelta(t, 5, k3.Fields["completion_ratio"].New, 1e-6)
	assert.InDelta(t, 0.1, k3.Fields["cache_ratio"].New, 1e-6)
	assert.InDelta(t, 1, k3.Fields["create_cache_ratio"].New, 1e-6)

	zeroCache := models["sync-test-zerocache"]
	assert.InDelta(t, 0, zeroCache.Fields["create_cache_ratio"].New, 1e-9, "explicit zero cache write must be written")
	perreq := models["sync-test-perreq"]
	assert.InDelta(t, 0.5, perreq.Fields["model_price"].New, 1e-9)

	_, groupBModel := models["sync-test-b-only"]
	assert.True(t, groupBModel, "models of every enabled group must be synced")

	skipped := map[string]string{}
	for _, entry := range plan.Skipped {
		skipped[entry.Name] = entry.Reason
	}
	assert.Equal(t, priceSyncSkipEmptyPricing, skipped["sync-test-empty"])
	assert.Equal(t, priceSyncSkipPriceConflict, skipped["sync-test-conflict"])
	assert.Equal(t, priceSyncSkipTieredExpr, skipped["sync-test-tiered"])
	assert.Equal(t, len(plan.Skipped), plan.Summary.ModelsSkipped)

	assert.Equal(t, "Group A", plan.UsableAdds["sync-test-a"])
	assert.Equal(t, "Group B", plan.UsableAdds["sync-test-b-remote"])

	// A disabled group is excluded from writes: it is not counted, its models
	// are not synced, it is not added to usable groups, and the rename override
	// still applies to its plan entry. With a single enabled source left the
	// conflict model resolves deterministically instead of being skipped.
	disabled := false
	disabledSource := source
	disabledSource.Groups = map[string]priceSyncGroupOverride{
		"5": {Name: "sync-test-b", Enabled: &disabled},
	}
	disabledPlan, err := buildPriceSyncPlan(disabledSource, groups)
	require.NoError(t, err)
	disabledGroups := map[int]priceSyncGroupPlan{}
	for _, group := range disabledPlan.Groups {
		disabledGroups[group.RemoteID] = group
	}
	assert.Equal(t, "sync-test-b", disabledGroups[5].LocalName, "rename override must win")
	assert.False(t, disabledGroups[5].Enabled)
	assert.Equal(t, 1, disabledPlan.Summary.GroupsCreated)
	disabledModels := map[string]priceSyncModelPlan{}
	for _, model := range disabledPlan.Models {
		disabledModels[model.Name] = model
	}
	_, disabledGroupModel := disabledModels["sync-test-b-only"]
	assert.False(t, disabledGroupModel, "models of a disabled group must not be synced")
	require.Contains(t, disabledModels, "sync-test-conflict")
	assert.InDelta(t, 5, disabledModels["sync-test-conflict"].Fields["model_ratio"].New, 1e-6)
	_, disabledGroupUsable := disabledPlan.UsableAdds["sync-test-b"]
	assert.False(t, disabledGroupUsable, "disabled groups are not added to usable groups")
	assert.Equal(t, "Group A", disabledPlan.UsableAdds["sync-test-a"])

	// Conversion scales absolute prices, markup scales group ratios; relative
	// multipliers (completion/cache) are price ratios and stay unchanged.
	scaled := source
	scaled.ConversionFactor = 2
	scaled.MarkupFactor = 1.2
	scaledPlan, err := buildPriceSyncPlan(scaled, groups)
	require.NoError(t, err)
	scaledModels := map[string]priceSyncModelPlan{}
	for _, model := range scaledPlan.Models {
		scaledModels[model.Name] = model
	}
	assert.InDelta(t, 20, scaledModels["sync-test-k3"].Fields["model_ratio"].New, 1e-6)
	assert.InDelta(t, 5, scaledModels["sync-test-k3"].Fields["completion_ratio"].New, 1e-6)
	assert.InDelta(t, 1, scaledModels["sync-test-k3"].Fields["create_cache_ratio"].New, 1e-6)
	assert.InDelta(t, 1, scaledModels["sync-test-perreq"].Fields["model_price"].New, 1e-9)
	scaledGroupA := map[int]priceSyncGroupPlan{}
	for _, group := range scaledPlan.Groups {
		scaledGroupA[group.RemoteID] = group
	}
	assert.InDelta(t, 0.54, scaledGroupA[31].NewRatio, 1e-9)
	assert.NotEqual(t, plan.Digest, scaledPlan.Digest)

	// The digest is deterministic for identical inputs so preview and apply agree.
	repeated, err := buildPriceSyncPlan(source, groups)
	require.NoError(t, err)
	assert.Equal(t, plan.Digest, repeated.Digest)
}

// The cost floor is group-level: sell = official price x cost rate x markup in
// the display currency, so a group is below cost exactly when its effective
// markup rounds below its cost rate. Group markup overrides beat the source
// markup, and the group ratio is divided by the site exchange rate so the
// invariant survives non-1 USD:CNY displays.
func TestBuildPriceSyncPlanCostFloor(t *testing.T) {
	source := priceSyncSource{
		ID:               "test",
		Name:             "test",
		URL:              "http://example.invalid/model-plaza",
		ConversionFactor: 1,
		MarkupFactor:     1.2,
		Groups: map[string]priceSyncGroupOverride{
			"5": {MarkupFactor: syncTestFloatPtr(0.5)},
		},
	}
	matched := syncTestPricing(4e-6, 2e-5, 2e-7, 5e-6)
	officialMatched := syncTestPricing(4e-6, 2e-5, 2e-7, 5e-6)
	officialMismatch := syncTestPricing(4e-7, 2e-6, 2e-8, 5e-7)
	groups := []priceSyncPlazaGroup{
		{ID: 31, Name: "floor-a", RateMultiplier: 0.75, Models: []priceSyncPlazaModel{
			{Name: "floor-matched", Pricing: matched, OfficialPricing: &officialMatched},
			{Name: "floor-mismatch", Pricing: matched, OfficialPricing: &officialMismatch},
			{Name: "floor-nocache", Pricing: priceSyncPlazaPricing{BillingMode: "token", InputPrice: syncTestFloatPtr(4e-6)}},
		}},
		{ID: 5, Name: "floor-b", RateMultiplier: 1.75, Models: []priceSyncPlazaModel{
			{Name: "floor-b-only", Pricing: syncTestPricing(1e-5, 1e-5, 0, 0)},
		}},
	}

	plan, err := buildPriceSyncPlan(source, groups)
	require.NoError(t, err)
	assert.InDelta(t, 1, plan.SiteRate, 1e-9, "default USD display means site rate 1")

	byID := map[int]priceSyncGroupPlan{}
	for _, group := range plan.Groups {
		byID[group.RemoteID] = group
	}
	groupA := byID[31]
	assert.InDelta(t, 0.75, groupA.CostRate, 1e-9)
	assert.InDelta(t, 1.2, groupA.Markup, 1e-9)
	assert.InDelta(t, 0.9, groupA.NewRatio, 1e-9)
	assert.False(t, groupA.BelowCost)
	groupB := byID[5]
	assert.InDelta(t, 0.5, groupB.Markup, 1e-9, "group markup override must beat the source markup")
	assert.InDelta(t, 0.875, groupB.NewRatio, 1e-9)
	assert.True(t, groupB.BelowCost, "markup 0.5 sells under the 1.75 cost rate")

	models := map[string]priceSyncModelPlan{}
	for _, model := range plan.Models {
		models[model.Name] = model
	}
	matchedModel := models["floor-matched"]
	require.NotNil(t, matchedModel.BasePriceUSDPer1M)
	assert.InDelta(t, 4, *matchedModel.BasePriceUSDPer1M, 1e-9)
	assert.True(t, matchedModel.Cache1hDerived, "synced 5m cache write implies a derived 1h price")
	assert.False(t, matchedModel.PriceBasisMismatch)
	assert.True(t, models["floor-mismatch"].PriceBasisMismatch)
	assert.False(t, models["floor-nocache"].Cache1hDerived)

	// With a CNY display at 7.3 the ratio is divided by the site rate so the
	// sell price still equals cost x markup in yuan.
	previousRate := operation_setting.USDExchangeRate
	t.Cleanup(func() { operation_setting.USDExchangeRate = previousRate })
	operation_setting.USDExchangeRate = 7.3
	previousDisplay := config.GlobalConfig.Get("general_setting").(*operation_setting.GeneralSetting).QuotaDisplayType
	t.Cleanup(func() {
		config.UpdateConfigFromMap(config.GlobalConfig.Get("general_setting"), map[string]string{"quota_display_type": previousDisplay})
	})
	config.UpdateConfigFromMap(config.GlobalConfig.Get("general_setting"), map[string]string{"quota_display_type": operation_setting.QuotaDisplayTypeCNY})

	cnyPlan, err := buildPriceSyncPlan(source, groups)
	require.NoError(t, err)
	assert.InDelta(t, 7.3, cnyPlan.SiteRate, 1e-9)
	for _, group := range cnyPlan.Groups {
		sellRate := group.NewRatio * cnyPlan.SiteRate
		// 6-decimal ratio rounding scaled by the site rate bounds the error.
		assert.InDelta(t, group.CostRate*group.Markup, sellRate, 1e-4, "sell = cost x markup must hold in CNY")
		assert.Equal(t, group.RemoteID == 5, group.BelowCost)
	}
	assert.NotEqual(t, plan.Digest, cnyPlan.Digest)
}

const priceSyncTestPlazaJSON = `{"code":0,"message":"success","data":{"groups":[
	{"id":31,"name":"sync-test-a","description":"Group A","rate_multiplier":0.45,"models":[
		{"name":"sync-test-k3","pricing":{"billing_mode":"token","input_price":2e-5,"output_price":1e-4,"cache_write_price":2e-5,"cache_read_price":2e-6,"image_input_price":null,"per_request_price":null,"intervals":[]}},
		{"name":"sync-test-empty","pricing":{"billing_mode":"token","input_price":null,"output_price":null,"cache_write_price":null,"cache_read_price":null,"image_input_price":null,"per_request_price":null,"intervals":[]}},
		{"name":"sync-test-perreq","pricing":{"billing_mode":"token","input_price":null,"output_price":null,"cache_write_price":null,"cache_read_price":null,"image_input_price":null,"per_request_price":0.5,"intervals":[]}}
	]},
	{"id":5,"name":"sync-test-b","description":"Group B","rate_multiplier":1.75,"models":[
		{"name":"sync-test-k3","pricing":{"billing_mode":"token","input_price":2e-5,"output_price":1e-4,"cache_write_price":2e-5,"cache_read_price":2e-6,"image_input_price":null,"per_request_price":null,"intervals":[]}},
		{"name":"sync-test-zerocache","pricing":{"billing_mode":"token","input_price":1e-5,"output_price":5e-5,"cache_write_price":0,"cache_read_price":1e-6,"image_input_price":null,"per_request_price":null,"intervals":[]}}
	]}
]}}`

type priceSyncTestErrorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type priceSyncTestSourcesResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Sources []priceSyncSourceView `json:"sources"`
	} `json:"data"`
}

type priceSyncTestPreviewResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Digest            string                  `json:"digest"`
		Groups            []priceSyncGroupPlan    `json:"groups"`
		Models            []priceSyncModelPlan    `json:"models"`
		Skipped           []priceSyncSkippedModel `json:"skipped"`
		Summary           priceSyncSummary        `json:"summary"`
		UsableGroupsAdded []string                `json:"usable_groups_added"`
	} `json:"data"`
}

type priceSyncTestApplyResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Applied    bool             `json:"applied"`
		SnapshotID string           `json:"snapshot_id"`
		Summary    priceSyncSummary `json:"summary"`
	} `json:"data"`
}

func TestPriceSyncSourceValidation(t *testing.T) {
	modelManagementDB(t, "sqlite", "")

	var listResponse priceSyncTestSourcesResponse
	modelManagementRequest(t, GetPriceSyncSources, http.MethodGet, "/api/price_sync/sources", nil, &listResponse)
	require.True(t, listResponse.Success)
	require.Len(t, listResponse.Data.Sources, 1)
	assert.Equal(t, priceSyncFufeiPresetID, listResponse.Data.Sources[0].ID)
	assert.Equal(t, priceSyncFufeiPresetURL, listResponse.Data.Sources[0].URL)

	var errorResponse priceSyncTestErrorResponse
	modelManagementRequest(t, UpdatePriceSyncSources, http.MethodPut, "/api/price_sync/sources", map[string]any{
		"sources": []map[string]any{{"id": "bad", "name": "bad", "url": "ftp://example.com", "conversion_factor": 1, "markup_factor": 1}},
	}, &errorResponse)
	assert.False(t, errorResponse.Success)
	assert.Equal(t, "Sync source URL is invalid", errorResponse.Message)

	modelManagementRequest(t, UpdatePriceSyncSources, http.MethodPut, "/api/price_sync/sources", map[string]any{
		"sources": []map[string]any{{"id": "bad", "name": "bad", "url": "http://example.com", "conversion_factor": 0, "markup_factor": 1}},
	}, &errorResponse)
	assert.Equal(t, "Conversion factor must be greater than 0", errorResponse.Message)

	modelManagementRequest(t, UpdatePriceSyncSources, http.MethodPut, "/api/price_sync/sources", map[string]any{
		"sources": []map[string]any{{
			"id": "bad", "name": "bad", "url": "http://example.com", "conversion_factor": 1, "markup_factor": 1,
			"groups": map[string]any{"5": map[string]any{"markup_factor": 0}},
		}},
	}, &errorResponse)
	assert.Equal(t, "Group markup factor must be greater than 0", errorResponse.Message)

	modelManagementRequest(t, UpdatePriceSyncSources, http.MethodPut, "/api/price_sync/sources", map[string]any{
		"sources": []map[string]any{
			{"id": "dup", "name": "dup", "url": "http://example.com", "conversion_factor": 1, "markup_factor": 1},
			{"id": "dup", "name": "dup-2", "url": "http://example.com", "conversion_factor": 1, "markup_factor": 1},
		},
	}, &errorResponse)
	assert.Equal(t, "Duplicate sync source id", errorResponse.Message)

	// The failed updates above must not have been persisted.
	var unchanged priceSyncTestSourcesResponse
	modelManagementRequest(t, GetPriceSyncSources, http.MethodGet, "/api/price_sync/sources", nil, &unchanged)
	require.Len(t, unchanged.Data.Sources, 1)
	assert.Equal(t, priceSyncFufeiPresetID, unchanged.Data.Sources[0].ID)

	// A source without an id gets one derived from its name.
	modelManagementRequest(t, UpdatePriceSyncSources, http.MethodPut, "/api/price_sync/sources", map[string]any{
		"sources": []map[string]any{{"name": "Derived Name", "url": "http://example.com", "conversion_factor": 1, "markup_factor": 1}},
	}, &listResponse)
	require.True(t, listResponse.Success)
	require.Len(t, listResponse.Data.Sources, 1)
	assert.Equal(t, "derived-name", listResponse.Data.Sources[0].ID)

	var reloaded priceSyncTestSourcesResponse
	modelManagementRequest(t, GetPriceSyncSources, http.MethodGet, "/api/price_sync/sources", nil, &reloaded)
	require.Len(t, reloaded.Data.Sources, 1)
	assert.Equal(t, "derived-name", reloaded.Data.Sources[0].ID)
}

func TestPriceSyncPreviewApplyAndSnapshots(t *testing.T) {
	modelManagementDB(t, "sqlite", "")

	plaza := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(priceSyncTestPlazaJSON))
	}))
	t.Cleanup(plaza.Close)

	var saveResponse priceSyncTestSourcesResponse
	modelManagementRequest(t, UpdatePriceSyncSources, http.MethodPut, "/api/price_sync/sources", map[string]any{
		"sources": []map[string]any{{"id": "test-src", "name": "test", "url": plaza.URL, "conversion_factor": 1, "markup_factor": 1}},
	}, &saveResponse)
	require.True(t, saveResponse.Success)

	var preview priceSyncTestPreviewResponse
	modelManagementRequest(t, PreviewPriceSync, http.MethodPost, "/api/price_sync/preview", map[string]any{"source_id": "test-src"}, &preview)
	require.True(t, preview.Success)
	require.NotEmpty(t, preview.Data.Digest)
	assert.Equal(t, 2, preview.Data.Summary.GroupsCreated)
	assert.Equal(t, 3, preview.Data.Summary.ModelsUpdated)
	assert.Equal(t, 1, preview.Data.Summary.ModelsSkipped)
	require.Len(t, preview.Data.Skipped, 1)
	assert.Equal(t, priceSyncSkipEmptyPricing, preview.Data.Skipped[0].Reason)
	assert.Contains(t, preview.Data.UsableGroupsAdded, "sync-test-a")
	assert.Contains(t, preview.Data.UsableGroupsAdded, "sync-test-b")

	var errorResponse priceSyncTestErrorResponse
	recorder := modelManagementRequest(t, ApplyPriceSync, http.MethodPost, "/api/price_sync/apply", map[string]any{
		"source_id": "test-src", "digest": "deadbeef",
	}, &errorResponse)
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Equal(t, "Source data changed, please preview again", errorResponse.Message)

	var applyResponse priceSyncTestApplyResponse
	modelManagementRequest(t, ApplyPriceSync, http.MethodPost, "/api/price_sync/apply", map[string]any{
		"source_id": "test-src", "digest": preview.Data.Digest,
	}, &applyResponse)
	require.True(t, applyResponse.Success)
	assert.True(t, applyResponse.Data.Applied)
	assert.NotEmpty(t, applyResponse.Data.SnapshotID)

	assert.InDelta(t, 10, ratio_setting.GetModelRatioCopy()["sync-test-k3"], 1e-6)
	assert.InDelta(t, 5, ratio_setting.GetCompletionRatioCopy()["sync-test-k3"], 1e-6)
	assert.InDelta(t, 0.1, ratio_setting.GetCacheRatioCopy()["sync-test-k3"], 1e-6)
	assert.InDelta(t, 1, ratio_setting.GetCreateCacheRatioCopy()["sync-test-k3"], 1e-6)
	assert.InDelta(t, 0, ratio_setting.GetCreateCacheRatioCopy()["sync-test-zerocache"], 1e-9)
	assert.InDelta(t, 0.5, ratio_setting.GetModelPriceCopy()["sync-test-perreq"], 1e-9)
	assert.InDelta(t, 0.45, ratio_setting.GetGroupRatioCopy()["sync-test-a"], 1e-9)
	assert.InDelta(t, 1.75, ratio_setting.GetGroupRatioCopy()["sync-test-b"], 1e-9)
	assert.Equal(t, "Group A", setting.GetUserUsableGroupsCopy()["sync-test-a"])

	snapshots := loadPriceSyncSnapshots()
	require.Len(t, snapshots, 1)
	assert.Equal(t, "test-src", snapshots[0].SourceID)
	assert.Equal(t, preview.Data.Digest, snapshots[0].Digest)

	// The apply itself changed the local state, so the old digest is now stale.
	recorder = modelManagementRequest(t, ApplyPriceSync, http.MethodPost, "/api/price_sync/apply", map[string]any{
		"source_id": "test-src", "digest": preview.Data.Digest,
	}, &errorResponse)
	assert.Equal(t, http.StatusConflict, recorder.Code)

	// A fresh preview shows nothing left to write and apply becomes a no-op.
	var previewAgain priceSyncTestPreviewResponse
	modelManagementRequest(t, PreviewPriceSync, http.MethodPost, "/api/price_sync/preview", map[string]any{"source_id": "test-src"}, &previewAgain)
	require.True(t, previewAgain.Success)
	assert.Empty(t, previewAgain.Data.Models)
	assert.Equal(t, 0, previewAgain.Data.Summary.ModelsUpdated)
	assert.Equal(t, 3, previewAgain.Data.Summary.ModelsUnchanged)
	assert.Equal(t, 0, previewAgain.Data.Summary.GroupsCreated)
	assert.Equal(t, 0, previewAgain.Data.Summary.GroupsUpdated)
	modelManagementRequest(t, ApplyPriceSync, http.MethodPost, "/api/price_sync/apply", map[string]any{
		"source_id": "test-src", "digest": previewAgain.Data.Digest,
	}, &applyResponse)
	require.True(t, applyResponse.Success)
	assert.False(t, applyResponse.Data.Applied)
	assert.Len(t, loadPriceSyncSnapshots(), 1, "a no-op apply must not record a new snapshot")

	var snapshotsResponse struct {
		Success bool `json:"success"`
		Data    struct {
			Snapshots []priceSyncSnapshot `json:"snapshots"`
		} `json:"data"`
	}
	modelManagementRequest(t, GetPriceSyncSnapshots, http.MethodGet, "/api/price_sync/snapshots", nil, &snapshotsResponse)
	require.True(t, snapshotsResponse.Success)
	assert.Len(t, snapshotsResponse.Data.Snapshots, 1)

	// Unknown source id is rejected cleanly.
	modelManagementRequest(t, PreviewPriceSync, http.MethodPost, "/api/price_sync/preview", map[string]any{"source_id": "nope"}, &errorResponse)
	assert.Equal(t, "Sync source not found", errorResponse.Message)
}

// An apply that would price an enabled group below its cost floor is rejected
// with 409 until the caller explicitly confirms with allow_below_cost.
func TestPriceSyncApplyBelowCost(t *testing.T) {
	modelManagementDB(t, "sqlite", "")

	// Distinct group names: ratio maps are process-global across tests.
	const belowCostPlazaJSON = `{"code":0,"message":"success","data":{"groups":[
		{"id":41,"name":"bc-test-a","description":"BC A","rate_multiplier":0.45,"models":[
			{"name":"bc-test-m","pricing":{"billing_mode":"token","input_price":2e-5,"output_price":1e-4,"cache_write_price":2e-5,"cache_read_price":2e-6,"intervals":[]}}
		]},
		{"id":42,"name":"bc-test-b","description":"BC B","rate_multiplier":1.75,"models":[]}
	]}}`
	plaza := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(belowCostPlazaJSON))
	}))
	t.Cleanup(plaza.Close)

	var saveResponse priceSyncTestSourcesResponse
	modelManagementRequest(t, UpdatePriceSyncSources, http.MethodPut, "/api/price_sync/sources", map[string]any{
		"sources": []map[string]any{{"id": "test-src", "name": "test", "url": plaza.URL, "conversion_factor": 1, "markup_factor": 0.5}},
	}, &saveResponse)
	require.True(t, saveResponse.Success)

	var preview priceSyncTestPreviewResponse
	modelManagementRequest(t, PreviewPriceSync, http.MethodPost, "/api/price_sync/preview", map[string]any{"source_id": "test-src"}, &preview)
	require.True(t, preview.Success)
	require.Len(t, preview.Data.Groups, 2)
	for _, group := range preview.Data.Groups {
		assert.True(t, group.BelowCost, "markup 0.5 must flag every group as below cost")
		assert.InDelta(t, 0.5, group.Markup, 1e-9)
		assert.InDelta(t, group.RateMultiplier, group.CostRate, 1e-9)
	}

	var errorResponse struct {
		Success         bool     `json:"success"`
		Message         string   `json:"message"`
		BelowCostGroups []string `json:"below_cost_groups"`
	}
	recorder := modelManagementRequest(t, ApplyPriceSync, http.MethodPost, "/api/price_sync/apply", map[string]any{
		"source_id": "test-src", "digest": preview.Data.Digest,
	}, &errorResponse)
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Equal(t, "Some groups would be priced below cost", errorResponse.Message)
	assert.ElementsMatch(t, []string{"bc-test-a", "bc-test-b"}, errorResponse.BelowCostGroups)
	_, written := ratio_setting.GetGroupRatioCopy()["bc-test-a"]
	assert.False(t, written, "a rejected apply must not write group ratios")

	var applyResponse priceSyncTestApplyResponse
	modelManagementRequest(t, ApplyPriceSync, http.MethodPost, "/api/price_sync/apply", map[string]any{
		"source_id": "test-src", "digest": preview.Data.Digest, "allow_below_cost": true,
	}, &applyResponse)
	require.True(t, applyResponse.Success)
	assert.True(t, applyResponse.Data.Applied)
	assert.InDelta(t, 0.225, ratio_setting.GetGroupRatioCopy()["bc-test-a"], 1e-9)
	assert.InDelta(t, 0.875, ratio_setting.GetGroupRatioCopy()["bc-test-b"], 1e-9)
}
