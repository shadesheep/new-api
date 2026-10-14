package controller

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

const (
	priceSyncSourcesOptionKey   = "PriceSyncSources"
	priceSyncSnapshotsOptionKey = "PriceSyncSnapshots"
	priceSyncMaxSnapshots       = 10
	priceSyncMaxSources         = 20
	priceSyncFetchTimeout       = 30 * time.Second

	priceSyncFufeiPresetID  = "fufei"
	priceSyncFufeiPresetURL = "https://fufei.mossx.ai/api/v1/model-plaza?timezone=Asia%2FShanghai"

	// Sanity bounds for externally sourced prices and factors. Values beyond
	// these are treated as broken upstream data and never reach the billing maps.
	priceSyncMaxFactor = 1e6
	priceSyncMaxPrice  = 1e6
	priceSyncMinInput  = 1e-12
)

// priceSyncFieldOptionKeys maps plan field names to their option keys. Option
// values are the whole ratio maps, so applies must write merged copies.
var priceSyncFieldOptionKeys = map[string]string{
	"model_ratio":        "ModelRatio",
	"completion_ratio":   "CompletionRatio",
	"cache_ratio":        "CacheRatio",
	"create_cache_ratio": "CreateCacheRatio",
	"image_ratio":        "ImageRatio",
	"model_price":        "ModelPrice",
}

// Skip reasons are stable codes; the web console renders the localized text.
const (
	priceSyncSkipEmptyPricing       = "empty_pricing"
	priceSyncSkipZeroInputPrice     = "zero_input_price"
	priceSyncSkipInvalidPricing     = "invalid_pricing"
	priceSyncSkipPriceConflict      = "price_conflict"
	priceSyncSkipTieredExpr         = "tiered_expr"
	priceSyncSkipUnsupportedPricing = "unsupported_pricing"
)

var priceSyncApplyMutex sync.Mutex

type priceSyncGroupOverride struct {
	Name    string `json:"name,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

type priceSyncSource struct {
	ID               string                            `json:"id"`
	Name             string                            `json:"name"`
	URL              string                            `json:"url"`
	Token            string                            `json:"token,omitempty"`
	ConversionFactor float64                           `json:"conversion_factor"`
	MarkupFactor     float64                           `json:"markup_factor"`
	Enabled          *bool                             `json:"enabled,omitempty"`
	Groups           map[string]priceSyncGroupOverride `json:"groups,omitempty"`
}

func (s priceSyncSource) isEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

func (g priceSyncGroupOverride) isEnabled() bool {
	return g.Enabled == nil || *g.Enabled
}

// Plaza upstream DTOs. Only the fields used for conversion are modeled.
type priceSyncPlazaGroup struct {
	ID             int                   `json:"id"`
	Name           string                `json:"name"`
	Description    string                `json:"description"`
	RateMultiplier float64               `json:"rate_multiplier"`
	Models         []priceSyncPlazaModel `json:"models"`
}

type priceSyncPlazaModel struct {
	Name    string                `json:"name"`
	Pricing priceSyncPlazaPricing `json:"pricing"`
}

type priceSyncPlazaPricing struct {
	BillingMode     string   `json:"billing_mode"`
	InputPrice      *float64 `json:"input_price"`
	OutputPrice     *float64 `json:"output_price"`
	CacheWritePrice *float64 `json:"cache_write_price"`
	CacheReadPrice  *float64 `json:"cache_read_price"`
	ImageInputPrice *float64 `json:"image_input_price"`
	PerRequestPrice *float64 `json:"per_request_price"`
	Intervals       []any    `json:"intervals"`
}

type priceSyncGroupPlan struct {
	RemoteID       int      `json:"remote_id"`
	RemoteName     string   `json:"remote_name"`
	Description    string   `json:"description,omitempty"`
	LocalName      string   `json:"local_name"`
	Enabled        bool     `json:"enabled"`
	RateMultiplier float64  `json:"rate_multiplier"`
	OldRatio       *float64 `json:"old_ratio"`
	NewRatio       float64  `json:"new_ratio"`
	Changed        bool     `json:"changed"`
	Created        bool     `json:"created"`
}

type priceSyncFieldChange struct {
	Old *float64 `json:"old"`
	New float64  `json:"new"`
}

type priceSyncModelPlan struct {
	Name   string                          `json:"name"`
	Fields map[string]priceSyncFieldChange `json:"fields"`
}

type priceSyncSkippedModel struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type priceSyncSummary struct {
	GroupsCreated   int `json:"groups_created"`
	GroupsUpdated   int `json:"groups_updated"`
	ModelsUpdated   int `json:"models_updated"`
	ModelsUnchanged int `json:"models_unchanged"`
	ModelsSkipped   int `json:"models_skipped"`
}

type priceSyncPlan struct {
	Digest     string                  `json:"digest"`
	Groups     []priceSyncGroupPlan    `json:"groups"`
	Models     []priceSyncModelPlan    `json:"models"`
	Skipped    []priceSyncSkippedModel `json:"skipped"`
	Summary    priceSyncSummary        `json:"summary"`
	UsableAdds map[string]string       `json:"-"`
}

type priceSyncSnapshot struct {
	ID         string                  `json:"id"`
	CreatedAt  int64                   `json:"created_at"`
	SourceID   string                  `json:"source_id"`
	SourceName string                  `json:"source_name"`
	Digest     string                  `json:"digest"`
	Summary    priceSyncSummary        `json:"summary"`
	Groups     []priceSyncGroupPlan    `json:"groups"`
	Models     []priceSyncModelPlan    `json:"models"`
	Skipped    []priceSyncSkippedModel `json:"skipped"`
}

type priceSyncKnownGroup struct {
	RemoteID   int    `json:"remote_id"`
	RemoteName string `json:"remote_name"`
	LocalName  string `json:"local_name"`
	Enabled    bool   `json:"enabled"`
}

type priceSyncSourceView struct {
	priceSyncSource
	KnownGroups []priceSyncKnownGroup `json:"known_groups,omitempty"`
}

func defaultPriceSyncSources() []priceSyncSource {
	return []priceSyncSource{{
		ID:               priceSyncFufeiPresetID,
		Name:             "fufei",
		URL:              priceSyncFufeiPresetURL,
		ConversionFactor: 1,
		MarkupFactor:     1,
	}}
}

// normalizePriceSyncSource repairs stored values so a broken row can never
// turn into a bad ratio; request validation rejects the same problems with a
// visible error before saving.
func normalizePriceSyncSource(source *priceSyncSource) {
	source.ID = strings.TrimSpace(source.ID)
	source.Name = strings.TrimSpace(source.Name)
	source.URL = strings.TrimSpace(source.URL)
	source.Token = strings.TrimSpace(source.Token)
	if source.ConversionFactor <= 0 || !isFiniteFactor(source.ConversionFactor) {
		source.ConversionFactor = 1
	}
	if source.MarkupFactor <= 0 || !isFiniteFactor(source.MarkupFactor) {
		source.MarkupFactor = 1
	}
	if source.Enabled == nil {
		enabled := true
		source.Enabled = &enabled
	}
	for key, override := range source.Groups {
		override.Name = strings.TrimSpace(override.Name)
		if override.Enabled == nil {
			enabled := true
			override.Enabled = &enabled
		}
		source.Groups[key] = override
	}
}

func isFiniteFactor(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= priceSyncMaxFactor
}

func loadPriceSyncSources() []priceSyncSource {
	common.OptionMapRWMutex.RLock()
	raw, exists := common.OptionMap[priceSyncSourcesOptionKey]
	common.OptionMapRWMutex.RUnlock()
	if !exists || strings.TrimSpace(raw) == "" {
		return defaultPriceSyncSources()
	}
	var sources []priceSyncSource
	if err := common.UnmarshalJsonStr(raw, &sources); err != nil {
		common.SysError(common.LogText("failed to parse price sync sources: %s", err.Error()))
		return defaultPriceSyncSources()
	}
	for i := range sources {
		normalizePriceSyncSource(&sources[i])
	}
	return sources
}

func savePriceSyncSources(sources []priceSyncSource) error {
	encoded, err := common.Marshal(sources)
	if err != nil {
		return err
	}
	return model.UpdateOption(priceSyncSourcesOptionKey, string(encoded))
}

func loadPriceSyncSnapshots() []priceSyncSnapshot {
	common.OptionMapRWMutex.RLock()
	raw, exists := common.OptionMap[priceSyncSnapshotsOptionKey]
	common.OptionMapRWMutex.RUnlock()
	if !exists || strings.TrimSpace(raw) == "" {
		return nil
	}
	var snapshots []priceSyncSnapshot
	if err := common.UnmarshalJsonStr(raw, &snapshots); err != nil {
		common.SysError(common.LogText("failed to parse price sync snapshots: %s", err.Error()))
		return nil
	}
	return snapshots
}

func findPriceSyncSource(id string) (priceSyncSource, bool) {
	for _, source := range loadPriceSyncSources() {
		if source.ID == id {
			return source, true
		}
	}
	return priceSyncSource{}, false
}

func priceSyncKnownGroups(sourceID string, snapshots []priceSyncSnapshot) []priceSyncKnownGroup {
	for _, snapshot := range snapshots {
		if snapshot.SourceID != sourceID {
			continue
		}
		known := make([]priceSyncKnownGroup, 0, len(snapshot.Groups))
		for _, group := range snapshot.Groups {
			known = append(known, priceSyncKnownGroup{
				RemoteID:   group.RemoteID,
				RemoteName: group.RemoteName,
				LocalName:  group.LocalName,
				Enabled:    group.Enabled,
			})
		}
		return known
	}
	return nil
}

func priceSyncSourceViews() []priceSyncSourceView {
	sources := loadPriceSyncSources()
	snapshots := loadPriceSyncSnapshots()
	views := make([]priceSyncSourceView, 0, len(sources))
	for _, source := range sources {
		views = append(views, priceSyncSourceView{
			priceSyncSource: source,
			KnownGroups:     priceSyncKnownGroups(source.ID, snapshots),
		})
	}
	return views
}

func GetPriceSyncSources(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"sources": priceSyncSourceViews()})
}

func UpdatePriceSyncSources(c *gin.Context) {
	var request struct {
		Sources []priceSyncSource `json:"sources"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid request parameter format"))
		return
	}
	if len(request.Sources) > priceSyncMaxSources {
		common.ApiErrorT(c, "Too many sync sources")
		return
	}
	seen := make(map[string]struct{}, len(request.Sources))
	for i := range request.Sources {
		source := &request.Sources[i]
		// Validate raw factors before normalization so a non-positive value is
		// rejected loudly instead of silently syncing with a default.
		if source.ConversionFactor <= 0 || !isFiniteFactor(source.ConversionFactor) {
			common.ApiErrorT(c, "Conversion factor must be greater than 0")
			return
		}
		if source.MarkupFactor <= 0 || !isFiniteFactor(source.MarkupFactor) {
			common.ApiErrorT(c, "Markup factor must be greater than 0")
			return
		}
		normalizePriceSyncSource(source)
		if source.ID == "" {
			source.ID = derivePriceSyncSourceID(source.Name, seen)
		}
		if _, duplicated := seen[source.ID]; duplicated {
			common.ApiErrorT(c, "Duplicate sync source id")
			return
		}
		seen[source.ID] = struct{}{}
		if source.Name == "" {
			common.ApiErrorT(c, "Sync source name is required")
			return
		}
		parsed, err := url.Parse(source.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			common.ApiErrorT(c, "Sync source URL is invalid")
			return
		}
	}
	if err := savePriceSyncSources(request.Sources); err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to save sync sources"))
		return
	}
	recordManageAudit(c, "price_sync.sources.update", map[string]any{"sources": len(request.Sources)})
	common.ApiSuccess(c, gin.H{"sources": priceSyncSourceViews()})
}

func derivePriceSyncSourceID(name string, seen map[string]struct{}) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r == ' ':
			return '-'
		default:
			return -1
		}
	}, base)
	base = strings.Trim(base, "-_")
	if base == "" {
		base = "source"
	}
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, exists := seen[candidate]; !exists {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func fetchPriceSyncPlazaGroups(ctx context.Context, source priceSyncSource) ([]priceSyncPlazaGroup, error) {
	parsed, err := url.Parse(source.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("invalid source URL")
	}
	ctx, cancel := context.WithTimeout(ctx, priceSyncFetchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	if source.Token != "" {
		request.Header.Set("Authorization", "Bearer "+source.Token)
	}
	transport := &http.Transport{
		MaxIdleConns:          16,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig.Clone()
	}
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRatioConfigBytes))
	if err != nil {
		return nil, err
	}
	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Groups []priceSyncPlazaGroup `json:"groups"`
		} `json:"data"`
	}
	if err := common.DecodeJson(bytes.NewReader(body), &payload); err != nil {
		return nil, errors.New("failed to parse the source response")
	}
	if payload.Code != 0 {
		if payload.Message == "" {
			payload.Message = "source rejected the request"
		}
		return nil, errors.New(payload.Message)
	}
	if len(payload.Data.Groups) == 0 {
		return nil, errors.New("source returned no groups")
	}
	return payload.Data.Groups, nil
}

// convertPriceSyncPricing converts one slot's pricing into local ratio fields.
// Conversion factors scale prices only; the relative multipliers
// (completion/cache/image) are ratios between two prices and stay unchanged.
func convertPriceSyncPricing(pricing priceSyncPlazaPricing, conversion float64) (map[string]float64, string) {
	if pricing.BillingMode == billing_setting.BillingModeTieredExpr || len(pricing.Intervals) > 0 {
		return nil, priceSyncSkipUnsupportedPricing
	}
	if pricing.PerRequestPrice != nil {
		price := *pricing.PerRequestPrice
		if !validSyncPrice(price) {
			return nil, priceSyncSkipInvalidPricing
		}
		return map[string]float64{"model_price": roundRatioValue(price * conversion)}, ""
	}
	if pricing.InputPrice == nil {
		return nil, priceSyncSkipEmptyPricing
	}
	input := *pricing.InputPrice
	if !validSyncPrice(input) {
		return nil, priceSyncSkipInvalidPricing
	}
	if input == 0 {
		return nil, priceSyncSkipZeroInputPrice
	}
	if input < priceSyncMinInput {
		return nil, priceSyncSkipInvalidPricing
	}
	fields := make(map[string]float64, 5)
	modelRatio := roundRatioValue(input * 1000 * float64(ratio_setting.USD) * conversion)
	if !validSyncMultiplier(modelRatio) {
		return nil, priceSyncSkipInvalidPricing
	}
	fields["model_ratio"] = modelRatio
	multipliers := []struct {
		field string
		price *float64
	}{
		{"completion_ratio", pricing.OutputPrice},
		{"cache_ratio", pricing.CacheReadPrice},
		{"create_cache_ratio", pricing.CacheWritePrice},
		{"image_ratio", pricing.ImageInputPrice},
	}
	for _, item := range multipliers {
		if item.price == nil {
			continue
		}
		if !validSyncPrice(*item.price) {
			return nil, priceSyncSkipInvalidPricing
		}
		ratio := roundRatioValue(*item.price / input)
		if !validSyncMultiplier(ratio) {
			return nil, priceSyncSkipInvalidPricing
		}
		fields[item.field] = ratio
	}
	return fields, ""
}

func validSyncPrice(price float64) bool {
	return !math.IsNaN(price) && !math.IsInf(price, 0) && price >= 0 && price <= priceSyncMaxPrice
}

func validSyncMultiplier(ratio float64) bool {
	return !math.IsNaN(ratio) && !math.IsInf(ratio, 0) && ratio >= 0 && ratio <= priceSyncMaxPrice
}

func priceSyncRatioMaps() map[string]map[string]float64 {
	return map[string]map[string]float64{
		"model_ratio":        ratio_setting.GetModelRatioCopy(),
		"completion_ratio":   ratio_setting.GetCompletionRatioCopy(),
		"cache_ratio":        ratio_setting.GetCacheRatioCopy(),
		"create_cache_ratio": ratio_setting.GetCreateCacheRatioCopy(),
		"image_ratio":        ratio_setting.GetImageRatioCopy(),
		"model_price":        ratio_setting.GetModelPriceCopy(),
	}
}

type priceSyncModelCandidate struct {
	fields map[string]float64
	reason string
}

// buildPriceSyncPlan is the pure core of preview and apply: given the source
// configuration and freshly fetched plaza groups it computes the effective
// local write set, the skipped entries with stable reason codes, and a digest
// over everything that was displayed.
func buildPriceSyncPlan(source priceSyncSource, groups []priceSyncPlazaGroup) (priceSyncPlan, error) {
	plan := priceSyncPlan{
		Groups:     make([]priceSyncGroupPlan, 0, len(groups)),
		UsableAdds: map[string]string{},
	}
	slices.SortFunc(groups, func(a, b priceSyncPlazaGroup) int { return cmp.Compare(a.ID, b.ID) })
	groupRatioLocal := ratio_setting.GetGroupRatioCopy()
	usableLocal := setting.GetUserUsableGroupsCopy()
	modelsByGroup := make([][]priceSyncPlazaModel, 0, len(groups))

	for _, group := range groups {
		override := source.Groups[fmt.Sprintf("%d", group.ID)]
		enabled := override.isEnabled()
		localName := override.Name
		if localName == "" {
			localName = strings.TrimSpace(group.Name)
		}
		if localName == "" {
			continue
		}
		if !validSyncMultiplier(group.RateMultiplier) {
			continue
		}
		newRatio := roundRatioValue(group.RateMultiplier * source.MarkupFactor)
		if !validSyncMultiplier(newRatio) {
			continue
		}
		entry := priceSyncGroupPlan{
			RemoteID:       group.ID,
			RemoteName:     group.Name,
			Description:    group.Description,
			LocalName:      localName,
			Enabled:        enabled,
			RateMultiplier: group.RateMultiplier,
			NewRatio:       newRatio,
		}
		if old, exists := groupRatioLocal[localName]; exists {
			value := old
			entry.OldRatio = &value
			entry.Changed = !nearlyEqual(old, newRatio)
		} else {
			entry.Created = true
			entry.Changed = true
		}
		plan.Groups = append(plan.Groups, entry)
		modelsByGroup = append(modelsByGroup, group.Models)
		if entry.Enabled && entry.Changed {
			if entry.Created {
				plan.Summary.GroupsCreated++
			} else {
				plan.Summary.GroupsUpdated++
			}
		}
		if entry.Enabled {
			if _, exists := usableLocal[localName]; !exists {
				description := strings.TrimSpace(group.Description)
				if description == "" {
					description = localName
				}
				plan.UsableAdds[localName] = description
			}
		}
	}

	candidates := map[string][]priceSyncModelCandidate{}
	billingModes := billing_setting.GetBillingModeCopy()
	for i, group := range plan.Groups {
		if !group.Enabled {
			continue
		}
		for _, model := range modelsByGroup[i] {
			name := strings.TrimSpace(model.Name)
			if name == "" {
				continue
			}
			if billingModes[name] == billing_setting.BillingModeTieredExpr {
				candidates[name] = append(candidates[name], priceSyncModelCandidate{reason: priceSyncSkipTieredExpr})
				continue
			}
			fields, reason := convertPriceSyncPricing(model.Pricing, source.ConversionFactor)
			candidates[name] = append(candidates[name], priceSyncModelCandidate{fields: fields, reason: reason})
		}
	}

	localRatioMaps := priceSyncRatioMaps()
	names := slices.Sorted(maps.Keys(candidates))
	for _, name := range names {
		entries := candidates[name]
		var valid []map[string]float64
		reasons := map[string]struct{}{}
		for _, entry := range entries {
			if entry.reason == "" {
				valid = append(valid, entry.fields)
			} else {
				reasons[entry.reason] = struct{}{}
			}
		}
		if len(valid) == 0 {
			plan.Skipped = append(plan.Skipped, priceSyncSkippedModel{Name: name, Reason: pickPriceSyncSkipReason(reasons)})
			continue
		}
		conflict := false
		for _, candidate := range valid[1:] {
			if !samePriceSyncFields(valid[0], candidate) {
				conflict = true
				break
			}
		}
		if conflict {
			plan.Skipped = append(plan.Skipped, priceSyncSkippedModel{Name: name, Reason: priceSyncSkipPriceConflict})
			continue
		}
		changes := map[string]priceSyncFieldChange{}
		for field, value := range valid[0] {
			local := localRatioMaps[field]
			old, exists := local[name]
			if exists && nearlyEqual(old, value) {
				continue
			}
			change := priceSyncFieldChange{New: value}
			if exists {
				previous := old
				change.Old = &previous
			}
			changes[field] = change
		}
		if len(changes) == 0 {
			plan.Summary.ModelsUnchanged++
			continue
		}
		plan.Models = append(plan.Models, priceSyncModelPlan{Name: name, Fields: changes})
		plan.Summary.ModelsUpdated++
	}
	plan.Summary.ModelsSkipped = len(plan.Skipped)

	payload, err := common.Marshal(struct {
		Groups  []priceSyncGroupPlan `json:"groups"`
		Models  []priceSyncModelPlan `json:"models"`
		Usables []string             `json:"usables"`
	}{plan.Groups, plan.Models, slices.Sorted(maps.Keys(plan.UsableAdds))})
	if err != nil {
		return priceSyncPlan{}, err
	}
	sum := sha256.Sum256(payload)
	plan.Digest = hex.EncodeToString(sum[:])
	return plan, nil
}

func pickPriceSyncSkipReason(reasons map[string]struct{}) string {
	for _, reason := range []string{
		priceSyncSkipEmptyPricing,
		priceSyncSkipZeroInputPrice,
		priceSyncSkipInvalidPricing,
		priceSyncSkipUnsupportedPricing,
		priceSyncSkipTieredExpr,
	} {
		if _, exists := reasons[reason]; exists {
			return reason
		}
	}
	return priceSyncSkipInvalidPricing
}

func samePriceSyncFields(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for field, value := range a {
		other, exists := b[field]
		if !exists || !nearlyEqual(value, other) {
			return false
		}
	}
	return true
}

func PreviewPriceSync(c *gin.Context) {
	var request struct {
		SourceID string `json:"source_id"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid request parameter format"))
		return
	}
	source, exists := findPriceSyncSource(strings.TrimSpace(request.SourceID))
	if !exists {
		common.ApiErrorT(c, "Sync source not found")
		return
	}
	if !source.isEnabled() {
		common.ApiErrorT(c, "Sync source is disabled")
		return
	}
	groups, err := fetchPriceSyncPlazaGroups(c.Request.Context(), source)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadGateway, common.NewMessage("Failed to fetch sync source"), gin.H{"detail": err.Error()})
		return
	}
	plan, err := buildPriceSyncPlan(source, groups)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to build the sync plan"))
		return
	}
	common.ApiSuccess(c, gin.H{
		"source":              priceSyncSourceView{priceSyncSource: source},
		"digest":              plan.Digest,
		"groups":              plan.Groups,
		"models":              plan.Models,
		"skipped":             plan.Skipped,
		"summary":             plan.Summary,
		"usable_groups_added": slices.Sorted(maps.Keys(plan.UsableAdds)),
	})
}

func ApplyPriceSync(c *gin.Context) {
	var request struct {
		SourceID string `json:"source_id"`
		Digest   string `json:"digest"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid request parameter format"))
		return
	}
	source, exists := findPriceSyncSource(strings.TrimSpace(request.SourceID))
	if !exists {
		common.ApiErrorT(c, "Sync source not found")
		return
	}
	if !source.isEnabled() {
		common.ApiErrorT(c, "Sync source is disabled")
		return
	}
	priceSyncApplyMutex.Lock()
	defer priceSyncApplyMutex.Unlock()
	groups, err := fetchPriceSyncPlazaGroups(c.Request.Context(), source)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadGateway, common.NewMessage("Failed to fetch sync source"), gin.H{"detail": err.Error()})
		return
	}
	plan, err := buildPriceSyncPlan(source, groups)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to build the sync plan"))
		return
	}
	if plan.Digest != request.Digest {
		common.ApiErrorStatus(c, http.StatusConflict, common.NewMessage("Source data changed, please preview again"))
		return
	}
	if err := applyPriceSyncPlan(plan); err != nil {
		common.SysError(common.LogText("failed to apply price sync: %s", err.Error()))
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to apply price sync"))
		return
	}
	snapshot := priceSyncSnapshot{
		CreatedAt:  time.Now().Unix(),
		SourceID:   source.ID,
		SourceName: source.Name,
		Digest:     plan.Digest,
		Summary:    plan.Summary,
		Groups:     plan.Groups,
		Models:     plan.Models,
		Skipped:    plan.Skipped,
	}
	applied := plan.Summary.GroupsCreated+plan.Summary.GroupsUpdated+plan.Summary.ModelsUpdated > 0 || len(plan.UsableAdds) > 0
	if applied {
		snapshot.ID = fmt.Sprintf("%s-%s", time.Now().Format("20060102T150405"), plan.Digest[:8])
		snapshots := append([]priceSyncSnapshot{snapshot}, loadPriceSyncSnapshots()...)
		if len(snapshots) > priceSyncMaxSnapshots {
			snapshots = snapshots[:priceSyncMaxSnapshots]
		}
		encoded, err := common.Marshal(snapshots)
		if err != nil {
			common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to record sync snapshot"))
			return
		}
		if err := model.UpdateOption(priceSyncSnapshotsOptionKey, string(encoded)); err != nil {
			common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to record sync snapshot"))
			return
		}
	}
	recordManageAudit(c, "price_sync.apply", map[string]any{
		"source":         source.ID,
		"groups_created": plan.Summary.GroupsCreated,
		"groups_updated": plan.Summary.GroupsUpdated,
		"models_updated": plan.Summary.ModelsUpdated,
	})
	common.ApiSuccess(c, gin.H{
		"applied":     applied,
		"snapshot_id": snapshot.ID,
		"summary":     plan.Summary,
	})
}

// applyPriceSyncPlan writes the plan through the same locked and validated
// paths the model pricing editor uses. Pricing maps commit first in one
// transaction, then group ratios and usable groups in a second one; a failure
// between the phases leaves a state the admin can heal by simply re-applying
// (applies are idempotent).
func applyPriceSyncPlan(plan priceSyncPlan) error {
	updates := map[string]string{}
	fieldWrites := map[string]map[string]float64{}
	for _, model := range plan.Models {
		for field, change := range model.Fields {
			optionKey := priceSyncFieldOptionKeys[field]
			if fieldWrites[optionKey] == nil {
				fieldWrites[optionKey] = map[string]float64{}
			}
			fieldWrites[optionKey][model.Name] = change.New
		}
	}
	for optionKey, writes := range fieldWrites {
		merged := priceSyncLocalRatioMap(optionKey)
		maps.Copy(merged, writes)
		encoded, err := common.Marshal(merged)
		if err != nil {
			return err
		}
		updates[optionKey] = string(encoded)
	}
	if len(updates) > 0 {
		if err := model.UpdateModelPricingOptions(updates); err != nil {
			return err
		}
	}

	bulk := map[string]string{}
	groupWrites := map[string]float64{}
	for _, group := range plan.Groups {
		if group.Enabled && group.Changed {
			groupWrites[group.LocalName] = group.NewRatio
		}
	}
	if len(groupWrites) > 0 {
		merged := ratio_setting.GetGroupRatioCopy()
		maps.Copy(merged, groupWrites)
		encoded, err := common.Marshal(merged)
		if err != nil {
			return err
		}
		bulk["GroupRatio"] = string(encoded)
	}
	if len(plan.UsableAdds) > 0 {
		merged := setting.GetUserUsableGroupsCopy()
		for name, description := range plan.UsableAdds {
			if _, exists := merged[name]; !exists {
				merged[name] = description
			}
		}
		encoded, err := common.Marshal(merged)
		if err != nil {
			return err
		}
		bulk["UserUsableGroups"] = string(encoded)
	}
	if len(bulk) > 0 {
		if err := model.UpdateOptionsBulk(bulk); err != nil {
			return err
		}
	}
	return nil
}

func priceSyncLocalRatioMap(optionKey string) map[string]float64 {
	switch optionKey {
	case "ModelRatio":
		return ratio_setting.GetModelRatioCopy()
	case "CompletionRatio":
		return ratio_setting.GetCompletionRatioCopy()
	case "CacheRatio":
		return ratio_setting.GetCacheRatioCopy()
	case "CreateCacheRatio":
		return ratio_setting.GetCreateCacheRatioCopy()
	case "ImageRatio":
		return ratio_setting.GetImageRatioCopy()
	case "ModelPrice":
		return ratio_setting.GetModelPriceCopy()
	default:
		return map[string]float64{}
	}
}

func GetPriceSyncSnapshots(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"snapshots": loadPriceSyncSnapshots()})
}
