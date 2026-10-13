package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPricingSyncExpressionPriority(t *testing.T) {
	expression := `tier("base", p * 2 + c * 8 + cr * 0)`
	cases := []struct {
		name       string
		local      map[string]any
		source     map[string]any
		wantFields []string
	}{
		{"equal expressions suppress stale ratios", map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": expression}}, map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": expression}, "model_ratio": map[string]float64{"m": 3}, "model_price": map[string]float64{"m": 2}}, nil},
		{"local expression excludes legacy-only source", map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": expression}}, map[string]any{"model_ratio": map[string]float64{"m": 3}, "completion_ratio": map[string]float64{"m": 2}}, nil},
		{"expression imports without legacy conflicts", map[string]any{"model_ratio": map[string]float64{"m": 1}}, map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": expression}, "model_ratio": map[string]float64{"m": 3}, "model_price": map[string]float64{"m": 2}}, []string{"billing_mode", "billing_expr"}},
		{"inactive expression follows explicit ratio mode", map[string]any{"model_ratio": map[string]float64{"m": 1}}, map[string]any{"billing_mode": map[string]string{"m": "ratio"}, "billing_expr": map[string]string{"m": expression}, "model_ratio": map[string]float64{"m": 3}}, []string{"model_ratio"}},
		{"empty active expression never imports a false free price", map[string]any{}, map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": " "}, "model_ratio": map[string]float64{"m": 0}}, nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			diff := buildDifferences(tt.local, []struct {
				name string
				data map[string]any
			}{{"source", tt.source}})
			fields := make([]string, 0, len(diff["m"]))
			for field := range diff["m"] {
				fields = append(fields, field)
			}
			assert.ElementsMatch(t, tt.wantFields, fields)
		})
	}
}

func TestRatioConfigExportsEffectiveExpressions(t *testing.T) {
	before := config.GlobalConfig.ExportAllConfigs()
	expose := ratio_setting.IsExposeRatioEnabled()
	t.Cleanup(func() {
		config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": before["billing_setting.billing_mode"], "billing_expr": before["billing_setting.billing_expr"]})
		ratio_setting.SetExposeRatioEnabled(expose)
	})
	config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": `{"sync-export":"tiered_expr"}`, "billing_expr": `{"sync-export":"tier(\"base\", p * 2)"}`})
	ratio_setting.SetExposeRatioEnabled(true)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/ratio_config", nil)
	GetRatioConfig(c)
	var response struct {
		Success bool
		Data    map[string]any
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, billing_setting.BillingModeTieredExpr, valueMap(response.Data["billing_mode"])["sync-export"])
	assert.Equal(t, `tier("base", p * 2)`, valueMap(response.Data["billing_expr"])["sync-export"])
}

func TestPricingSyncCompleteSourcesAndArrayFormats(t *testing.T) {
	before := config.GlobalConfig.ExportAllConfigs()
	oldRatios, oldCompletion := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	t.Cleanup(func() {
		config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": before["billing_setting.billing_mode"], "billing_expr": before["billing_setting.billing_expr"]})
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"sync-token":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"sync-token":2}`))
	expression := `len <= 200000 ? tier("short", p * 2 + c * 8 + cr * 0) : tier("long", p * 4 + c * 12)`
	expressions, err := common.Marshal(map[string]string{"sync-already": expression})
	require.NoError(t, err)
	config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": `{"sync-already":"tiered_expr"}`, "billing_expr": string(expressions)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		if r.URL.Path == "/ratio_config" {
			data = map[string]any{
				"billing_mode":     map[string]string{"sync-already": "tiered_expr", "sync-expression": "tiered_expr"},
				"billing_expr":     map[string]string{"sync-already": expression, "sync-expression": expression},
				"model_ratio":      map[string]float64{"sync-already": 9, "sync-expression": 9, "sync-token": 1},
				"model_price":      map[string]float64{"sync-expression": 4},
				"completion_ratio": map[string]float64{"sync-token": 4},
				"cache_ratio":      map[string]float64{"sync-token": 0},
			}
		} else {
			data = []map[string]any{
				{"model_name": "sync-already", "model_ratio": 5, "completion_ratio": 3},
				{"model_name": "sync-expression", "model_ratio": 2, "model_price": 1},
				{"model_name": "sync-array-expression", "billing_mode": "tiered_expr", "billing_expr": expression, "quota_type": 1, "model_price": 0},
				{"model_name": "sync-unpriced"},
				{"model_name": "sync-invalid-expression", "billing_mode": "tiered_expr", "billing_expr": "", "model_ratio": 0},
				{"model_name": "sync-free", "model_ratio": 0, "completion_ratio": 0},
			}
		}
		encoded, err := common.Marshal(map[string]any{"success": true, "data": data})
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(encoded)
	}))
	defer server.Close()
	var response struct {
		Success bool
		Data    struct {
			Differences map[string]map[string]dto.DifferenceItem
			Prices      map[string]struct {
				Current   map[string]any
				Upstreams map[string]map[string]any
			}
			TestResults []dto.TestResult `json:"test_results"`
		}
	}
	body := map[string]any{"upstreams": []map[string]any{
		{"id": 1, "name": "Expressions", "base_url": server.URL, "endpoint": "/ratio_config"},
		{"id": 2, "name": "Legacy", "base_url": server.URL, "endpoint": "/pricing"},
	}}
	recorder := modelManagementRequest(t, FetchUpstreamRatios, http.MethodPost, "/api/channel/fetch_upstream_ratios", body, &response)
	require.True(t, response.Success, recorder.Body.String())
	require.Len(t, response.Data.TestResults, 2)
	for _, result := range response.Data.TestResults {
		require.Equal(t, "success", result.Status, result.Error)
	}
	assert.NotContains(t, response.Data.Differences, "sync-already")
	assert.NotContains(t, response.Data.Differences, "sync-unpriced")
	assert.NotContains(t, response.Data.Differences, "sync-invalid-expression")
	assert.Equal(t, map[string]any{"billing_mode": "tiered_expr", "billing_expr": expression}, response.Data.Prices["sync-expression"].Upstreams["Expressions(1)"])
	assert.NotContains(t, response.Data.Prices["sync-expression"].Upstreams, "Legacy(2)")
	assert.Equal(t, map[string]any{"billing_mode": "tiered_expr", "billing_expr": expression}, response.Data.Prices["sync-array-expression"].Upstreams["Legacy(2)"])
	assert.Equal(t, float64(1), response.Data.Prices["sync-token"].Upstreams["Expressions(1)"]["model_ratio"], "unchanged base prices are included for a complete price preview")
	assert.Equal(t, float64(4), response.Data.Prices["sync-token"].Upstreams["Expressions(1)"]["completion_ratio"])
	assert.Equal(t, float64(0), response.Data.Prices["sync-token"].Upstreams["Expressions(1)"]["cache_ratio"])
	assert.Equal(t, float64(0), response.Data.Prices["sync-free"].Upstreams["Legacy(2)"]["model_ratio"])
}

func TestConvertModelPlazaToRatioData(t *testing.T) {
	payload := `{
		"code": 0,
		"message": "success",
		"data": {
			"groups": [
				{
					"id": 1,
					"name": "pro",
					"models": [
						{"name": "plaza-pro", "pricing": {"billing_mode": "token", "input_price": 0.000002, "output_price": 0.00001, "cache_write_price": 2.5e-7, "cache_read_price": 2e-7},
						 "official_pricing": {"input_price": 0.000004, "output_price": 0.000016, "cache_write_price": 5e-7, "cache_read_price": 4e-7}},
						{"name": "plaza-free", "pricing": {"billing_mode": "token", "input_price": 0, "output_price": 0}, "official_pricing": null},
						{"name": "plaza-no-pricing", "pricing": null, "official_pricing": null},
						{"name": "plaza-request", "pricing": {"billing_mode": "per_request", "per_request_price": 0.05}, "official_pricing": null},
						{"name": "plaza-zero-cache", "pricing": {"billing_mode": "token", "input_price": 0.000008, "output_price": 0.000028, "cache_write_price": 0, "cache_read_price": 0.000002}, "official_pricing": null}
					]
				},
				{
					"id": 2,
					"name": "enterprise",
					"models": [
						{"name": "plaza-pro", "pricing": {"billing_mode": "token", "input_price": 0.00001, "output_price": 0.00005},
						 "official_pricing": {"input_price": 0.000004, "output_price": 0.000016, "cache_write_price": 5e-7, "cache_read_price": 4e-7}},
						{"name": "plaza-skip", "pricing": {"billing_mode": "token", "input_price": 0, "output_price": 0.00003}, "official_pricing": null},
						{"name": "plaza-negative", "pricing": {"billing_mode": "token", "input_price": -1, "output_price": 1}, "official_pricing": null}
					]
				}
			]
		}
	}`

	sell, official, err := convertModelPlazaToRatioData(strings.NewReader(payload))
	require.NoError(t, err)

	// Selling prices: 1 ratio unit = $0.002/1K tokens, so $2/M -> 1.
	assert.Equal(t, 1.0, sell["model_ratio"].(map[string]any)["plaza-pro"], "cheapest non-zero input price wins across groups")
	assert.Equal(t, 5.0, sell["completion_ratio"].(map[string]any)["plaza-pro"])
	assert.Equal(t, 0.1, sell["cache_ratio"].(map[string]any)["plaza-pro"])
	assert.Equal(t, 0.125, sell["create_cache_ratio"].(map[string]any)["plaza-pro"])
	assert.Equal(t, 0.0, sell["model_ratio"].(map[string]any)["plaza-free"])
	assert.Equal(t, 0.05, sell["model_price"].(map[string]any)["plaza-request"])
	assert.Equal(t, 0.0, sell["create_cache_ratio"].(map[string]any)["plaza-zero-cache"], "explicit zero cache write is preserved")
	for _, model := range []string{"plaza-no-pricing", "plaza-skip", "plaza-negative"} {
		assert.NotContains(t, sell["model_ratio"], model)
	}

	// Official prices use the official_pricing column.
	assert.Equal(t, 2.0, official["model_ratio"].(map[string]any)["plaza-pro"])
	assert.Equal(t, 4.0, official["completion_ratio"].(map[string]any)["plaza-pro"])
	assert.NotContains(t, official["model_ratio"], "plaza-free", "models without official pricing are absent")

	_, _, err = convertModelPlazaToRatioData(strings.NewReader(`{"code": 401, "message": "unauthorized"}`))
	require.Error(t, err)
	_, _, err = convertModelPlazaToRatioData(strings.NewReader(`not json`))
	require.Error(t, err)
}

func TestModelPlazaSync(t *testing.T) {
	plazaPayload := map[string]any{
		"code":    0,
		"message": "success",
		"data": map[string]any{
			"groups": []map[string]any{
				{
					"id":   5,
					"name": "pro",
					"models": []map[string]any{
						{"name": "plaza-token", "pricing": map[string]any{"billing_mode": "token", "input_price": 0.000002, "output_price": 0.00001, "cache_read_price": 2e-7},
							"official_pricing": map[string]any{"input_price": 0.000004, "output_price": 0.000016}},
					},
				},
			},
		},
	}
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/model-plaza", r.URL.Path)
		gotAuthorization = r.Header.Get("Authorization")
		encoded, err := common.Marshal(plazaPayload)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(encoded)
	}))
	defer server.Close()

	var response struct {
		Success bool
		Data    struct {
			Differences map[string]map[string]dto.DifferenceItem
			Prices      map[string]struct {
				Current   map[string]any
				Upstreams map[string]map[string]any
			}
			TestResults []dto.TestResult `json:"test_results"`
		}
	}
	body := map[string]any{"upstreams": []map[string]any{
		{"name": "Plaza", "base_url": server.URL, "endpoint": "model-plaza", "token": "test-plaza-token"},
	}}
	recorder := modelManagementRequest(t, FetchUpstreamRatios, http.MethodPost, "/api/channel/fetch_upstream_ratios", body, &response)
	require.True(t, response.Success, recorder.Body.String())
	assert.Equal(t, "Bearer test-plaza-token", gotAuthorization)

	names := make([]string, 0, len(response.Data.TestResults))
	for _, result := range response.Data.TestResults {
		require.Equal(t, "success", result.Status, result.Error)
		names = append(names, result.Name)
	}
	assert.ElementsMatch(t, []string{"Plaza|sell", "Plaza|official"}, names)

	require.Contains(t, response.Data.Prices, "plaza-token")
	upstreams := response.Data.Prices["plaza-token"].Upstreams
	assert.Equal(t, float64(1), upstreams["Plaza|sell"]["model_ratio"])
	assert.Equal(t, float64(5), upstreams["Plaza|sell"]["completion_ratio"])
	assert.Equal(t, 0.1, upstreams["Plaza|sell"]["cache_ratio"])
	assert.Equal(t, float64(2), upstreams["Plaza|official"]["model_ratio"])
	assert.Equal(t, float64(4), upstreams["Plaza|official"]["completion_ratio"])
}
