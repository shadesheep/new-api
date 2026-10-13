package controller

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDeepSeekBalanceUSD(t *testing.T) {
	tests := []struct {
		name            string
		responseJSON    string
		usdExchangeRate float64
		want            float64
		wantErrContains string
	}{
		{
			name:            "prefers USD when USD precedes CNY",
			responseJSON:    `{"balance_infos":[{"currency":"USD","total_balance":"12.50"},{"currency":"CNY","total_balance":"73.00"}]}`,
			usdExchangeRate: 7.3,
			want:            12.5,
		},
		{
			name:            "prefers USD when CNY precedes USD",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"73.00"},{"currency":"USD","total_balance":"12.50"}]}`,
			usdExchangeRate: 7.3,
			want:            12.5,
		},
		{
			name:            "converts CNY when USD is absent",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"73.00"}]}`,
			usdExchangeRate: 7.3,
			want:            10,
		},
		{
			name:            "returns error when USD and CNY are absent",
			responseJSON:    `{"balance_infos":[{"currency":"EUR","total_balance":"10.00"}]}`,
			usdExchangeRate: 7.3,
			wantErrContains: "currency USD or CNY not found",
		},
		{
			name:            "returns USD parse error instead of falling back to CNY",
			responseJSON:    `{"balance_infos":[{"currency":"USD","total_balance":"invalid"},{"currency":"CNY","total_balance":"73.00"}]}`,
			usdExchangeRate: 7.3,
			wantErrContains: "invalid syntax",
		},
		{
			name:            "rejects NaN USD balance",
			responseJSON:    `{"balance_infos":[{"currency":"USD","total_balance":"NaN"}]}`,
			usdExchangeRate: 7.3,
			wantErrContains: "USD balance must be finite",
		},
		{
			name:            "rejects negative USD balance",
			responseJSON:    `{"balance_infos":[{"currency":"USD","total_balance":"-1.00"}]}`,
			usdExchangeRate: 7.3,
			wantErrContains: "USD balance must be non-negative",
		},
		{
			name:            "rejects positive infinity CNY balance",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"+Inf"}]}`,
			usdExchangeRate: 7.3,
			wantErrContains: "CNY balance must be finite",
		},
		{
			name:            "rejects negative CNY balance",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"-7.30"}]}`,
			usdExchangeRate: 7.3,
			wantErrContains: "CNY balance must be non-negative",
		},
		{
			name:            "returns error for non-positive CNY exchange rate",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"73.00"}]}`,
			usdExchangeRate: 0,
			wantErrContains: "USD exchange rate must be greater than zero",
		},
		{
			name:            "rejects NaN CNY exchange rate",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"73.00"}]}`,
			usdExchangeRate: math.NaN(),
			wantErrContains: "USD exchange rate must be finite",
		},
		{
			name:            "rejects positive infinity CNY exchange rate",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"73.00"}]}`,
			usdExchangeRate: math.Inf(1),
			wantErrContains: "USD exchange rate must be finite",
		},
		{
			name:            "rejects CNY conversion overflow",
			responseJSON:    `{"balance_infos":[{"currency":"CNY","total_balance":"1.7976931348623157e+308"}]}`,
			usdExchangeRate: math.SmallestNonzeroFloat64,
			wantErrContains: "converted USD balance must be finite",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var response DeepSeekUsageResponse
			require.NoError(t, common.Unmarshal([]byte(test.responseJSON), &response))

			balance, err := getDeepSeekBalanceUSD(response, test.usdExchangeRate)
			if test.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.wantErrContains)
				return
			}

			require.NoError(t, err)
			assert.InDelta(t, test.want, balance, 1e-12)
		})
	}
}

func TestGetKimiCodingPlanBalance(t *testing.T) {
	tests := []struct {
		name                string
		responseJSON        string
		want                float64
		wantMatched         bool
		wantErrContains     string
		wantFiveHourPercent *float64
		wantFiveHourReset   string
		wantWeeklyPercent   float64
		wantWeeklyReset     string
	}{
		{
			name: "parses weekly remaining and both quota windows",
			responseJSON: `{
				"usage": {"limit": "100", "used": "35", "remaining": "65", "resetTime": "2026-09-29T01:16:59Z"},
				"limits": [{"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"}, "detail": {"limit": "100", "used": "8", "remaining": "92", "resetTime": "2026-09-23T11:16:59Z"}}],
				"usages": {"limit_5h": {"used_ratio": 0.084046, "reset_time": "2026-09-23T11:16:59Z"}, "limit_7d": {"used_ratio": 0.351027, "reset_time": "2026-09-29T01:16:59Z"}}
			}`,
			want:                65,
			wantMatched:         true,
			wantFiveHourPercent: common.GetPointer(8.4046),
			wantFiveHourReset:   "2026-09-23T11:16:59Z",
			wantWeeklyPercent:   35.1027,
			wantWeeklyReset:     "2026-09-29T01:16:59Z",
		},
		{
			name:              "matches exhausted plan with zero remaining",
			responseJSON:      `{"usage": {"limit": "100", "used": "100", "remaining": "0"}, "usages": {"limit_7d": {"used_ratio": 1, "reset_time": "2026-09-29T01:16:59Z"}}}`,
			want:              0,
			wantMatched:       true,
			wantWeeklyPercent: 100,
			wantWeeklyReset:   "2026-09-29T01:16:59Z",
		},
		{
			name: "derives remaining from limit and used when remaining is absent",
			responseJSON: `{
				"usage": {"limit": "100", "used": "100", "resetTime": "2026-10-15T03:24:56.392065Z"},
				"limits": [{"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"}, "detail": {"limit": "100", "used": "6", "remaining": "94", "resetTime": "2026-10-13T08:24:56.392065Z"}}],
				"usages": {"limit_5h": {"used_ratio": 0.0633, "reset_time": "2026-10-13T08:24:55Z"}, "limit_7d": {"used_ratio": 1, "reset_time": "2026-10-15T03:24:56Z"}}
			}`,
			want:                0,
			wantMatched:         true,
			wantFiveHourPercent: common.GetPointer(6.33),
			wantFiveHourReset:   "2026-10-13T08:24:55Z",
			wantWeeklyPercent:   100,
			wantWeeklyReset:     "2026-10-15T03:24:56Z",
		},
		{
			name:              "clamps derived remaining at zero when used exceeds limit",
			responseJSON:      `{"usage": {"limit": "100", "used": "120"}, "usages": {"limit_7d": {"used_ratio": 1.2, "reset_time": "2026-09-29T01:16:59Z"}}}`,
			want:              0,
			wantMatched:       true,
			wantWeeklyPercent: 120,
			wantWeeklyReset:   "2026-09-29T01:16:59Z",
		},
		{
			name:            "returns parse error for non-numeric limit without remaining",
			responseJSON:    `{"usage": {"limit": "abc", "used": "35"}, "usages": {"limit_7d": {"used_ratio": 0.35}}}`,
			wantMatched:     true,
			wantErrContains: "invalid syntax",
		},
		{
			name:         "does not match when weekly ratio is absent",
			responseJSON: `{"usage": {"limit": "100", "remaining": "65"}, "usages": {"limit_5h": {"used_ratio": 0.1}}}`,
			wantMatched:  false,
		},
		{
			name:         "does not match when weekly remaining is absent",
			responseJSON: `{"usage": {"limit": "100"}, "usages": {"limit_7d": {"used_ratio": 0.35}}}`,
			wantMatched:  false,
		},
		{
			name:         "does not match null weekly ratio",
			responseJSON: `{"usage": {"remaining": "65"}, "usages": {"limit_7d": null}}`,
			wantMatched:  false,
		},
		{
			name:         "does not match credit_summary response",
			responseJSON: `{"object": "credit_summary", "total_available": 12.5}`,
			wantMatched:  false,
		},
		{
			name:            "returns parse error for non-numeric remaining",
			responseJSON:    `{"usage": {"remaining": "abc"}, "usages": {"limit_7d": {"used_ratio": 0.35}}}`,
			wantMatched:     true,
			wantErrContains: "invalid syntax",
		},
		{
			name:            "rejects negative remaining",
			responseJSON:    `{"usage": {"remaining": "-5"}, "usages": {"limit_7d": {"used_ratio": 1.05}}}`,
			wantMatched:     true,
			wantErrContains: "must be non-negative",
		},
		{
			name:            "rejects non-finite remaining",
			responseJSON:    `{"usage": {"remaining": "NaN"}, "usages": {"limit_7d": {"used_ratio": 0.35}}}`,
			wantMatched:     true,
			wantErrContains: "must be finite",
		},
		{
			name:            "rejects negative five-hour used ratio",
			responseJSON:    `{"usage": {"remaining": "65"}, "usages": {"limit_5h": {"used_ratio": -0.1}, "limit_7d": {"used_ratio": 0.35}}}`,
			wantMatched:     true,
			wantErrContains: "five-hour window",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			balance, planUsage, matched, err := getKimiCodingPlanBalance([]byte(test.responseJSON))
			assert.Equal(t, test.wantMatched, matched)
			if test.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.wantErrContains)
				return
			}

			require.NoError(t, err)
			if !matched {
				assert.Nil(t, planUsage)
				return
			}
			assert.InDelta(t, test.want, balance, 1e-12)
			require.NotNil(t, planUsage)
			require.NotNil(t, planUsage.Weekly)
			assert.InDelta(t, test.wantWeeklyPercent, planUsage.Weekly.UsedPercent, 1e-9)
			assert.Equal(t, test.wantWeeklyReset, planUsage.Weekly.ResetTime)
			if test.wantFiveHourPercent == nil {
				assert.Nil(t, planUsage.FiveHour)
			} else {
				require.NotNil(t, planUsage.FiveHour)
				assert.InDelta(t, *test.wantFiveHourPercent, planUsage.FiveHour.UsedPercent, 1e-9)
				assert.Equal(t, test.wantFiveHourReset, planUsage.FiveHour.ResetTime)
			}
		})
	}
}
