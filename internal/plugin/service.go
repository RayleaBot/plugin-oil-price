package plugin

import (
	"context"
	"fmt"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	juheSecretKey = "juhe_api_key"
	amapSecretKey = "amap_api_key"
)

type hostActions interface {
	cacheActions
	SecretRead(context.Context, string) (rayleabot.ActionResult, error)
}

type service struct {
	settings settings
	provider providerClient
	now      func() time.Time
}

func newService(config settings, doer httpDoer, now func() time.Time) *service {
	if doer == nil {
		doer = newProviderHTTPClient()
	}
	if now == nil {
		now = time.Now
	}
	return &service{settings: config, provider: providerClient{http: doer}, now: now}
}

func (svc *service) query(ctx context.Context, actions hostActions, input queryInput) (queryResult, error) {
	now := svc.now()
	prices, priceStoredAt, priceStale, warnings, err := svc.loadPrices(ctx, actions, now)
	if err != nil {
		return queryResult{}, err
	}

	quote, directMatch := findRegionalPrice(prices, input.Area)
	amapKey, amapErr := readSecret(ctx, actions, amapSecretKey)
	if amapErr != nil {
		warnings = append(warnings, "高德密钥读取失败，未查询加油站")
	}

	area := areaResolution{Requested: input.Area}
	if directMatch {
		area.Province = quote.Region
	}
	if amapKey != "" {
		resolved, areaWarning, resolveErr := svc.loadArea(ctx, actions, amapKey, input.Area, now)
		if resolveErr == nil {
			area = resolved
			if matched, ok := findRegionalPrice(prices, resolved.Province); ok {
				quote = matched
				directMatch = true
			}
		} else if !directMatch {
			return queryResult{}, resolveErr
		} else {
			warnings = append(warnings, resolveErr.Error())
		}
		if areaWarning != "" {
			warnings = append(warnings, areaWarning)
		}
	}
	if !directMatch {
		if amapKey == "" {
			return queryResult{}, fmt.Errorf("查询城市需要先配置插件 secret %s", amapSecretKey)
		}
		return queryResult{}, fmt.Errorf("油价数据源未返回“%s”所属省份的参考价", input.Area)
	}
	if area.Province == "" {
		area.Province = quote.Region
	}

	result := queryResult{
		Area:          area,
		Price:         quote,
		PriceStoredAt: priceStoredAt,
		PriceStale:    priceStale,
		QueriedAt:     now,
		Warnings:      warnings,
	}
	if amapKey == "" {
		result.Warnings = append(result.Warnings, "未配置 amap_api_key，仅返回地区参考油价")
		return result, nil
	}
	stations, storedAt, stale, stationWarnings := svc.loadStations(ctx, actions, amapKey, area, input, now)
	result.Stations = stations
	result.StationStored = storedAt
	result.StationStale = stale
	result.Warnings = append(result.Warnings, stationWarnings...)
	return result, nil
}

func (svc *service) loadPrices(ctx context.Context, actions hostActions, now time.Time) ([]regionalPrice, time.Time, bool, []string, error) {
	cached, exists, cacheErr := readCache[[]regionalPrice](ctx, actions, priceCacheKey)
	warnings := make([]string, 0, 2)
	if cacheErr != nil {
		warnings = append(warnings, "地区油价缓存不可用")
	}
	if exists && cacheFresh(cached.StoredAt, now, svc.settings.PriceCacheTTL) {
		return cached.Data, cached.StoredAt, false, warnings, nil
	}
	apiKey, secretErr := readSecret(ctx, actions, juheSecretKey)
	if secretErr != nil || apiKey == "" {
		if exists {
			warnings = append(warnings, "油价数据源不可用，已返回最近缓存")
			return cached.Data, cached.StoredAt, true, warnings, nil
		}
		if secretErr != nil {
			return nil, time.Time{}, false, warnings, fmt.Errorf("读取油价密钥失败")
		}
		return nil, time.Time{}, false, warnings, fmt.Errorf("请先配置插件 secret %s", juheSecretKey)
	}
	prices, fetchErr := svc.provider.fetchPrices(ctx, apiKey)
	if fetchErr != nil {
		if exists {
			warnings = append(warnings, "油价数据源请求失败，已返回最近缓存")
			return cached.Data, cached.StoredAt, true, warnings, nil
		}
		return nil, time.Time{}, false, warnings, fetchErr
	}
	envelope := cacheEnvelope[[]regionalPrice]{StoredAt: now, Data: prices}
	if err := writeCache(ctx, actions, priceCacheKey, envelope); err != nil {
		warnings = append(warnings, "本次油价未能写入缓存")
	}
	return prices, now, false, warnings, nil
}

func (svc *service) loadArea(ctx context.Context, actions hostActions, apiKey, requested string, now time.Time) (areaResolution, string, error) {
	key := cacheKey("area", normalizeText(requested))
	cached, exists, cacheErr := readCache[areaResolution](ctx, actions, key)
	warning := ""
	if cacheErr != nil {
		warning = "地区解析缓存不可用"
	}
	if exists && cacheFresh(cached.StoredAt, now, svc.settings.AreaCacheTTL) {
		return cached.Data, warning, nil
	}
	resolved, err := svc.provider.resolveArea(ctx, apiKey, requested)
	if err != nil {
		if exists {
			return cached.Data, "地区解析服务不可用，已使用最近缓存", nil
		}
		return areaResolution{}, warning, err
	}
	if err := writeCache(ctx, actions, key, cacheEnvelope[areaResolution]{StoredAt: now, Data: resolved}); err != nil {
		warning = "本次地区解析未能写入缓存"
	}
	return resolved, warning, nil
}

func (svc *service) loadStations(ctx context.Context, actions hostActions, apiKey string, area areaResolution, input queryInput, now time.Time) ([]station, time.Time, bool, []string) {
	key := cacheKey("station", normalizeText(area.Requested), input.BrandID, fmt.Sprintf("%d", svc.settings.StationLimit))
	cached, exists, cacheErr := readCache[[]station](ctx, actions, key)
	warnings := make([]string, 0, 2)
	if cacheErr != nil {
		warnings = append(warnings, "加油站缓存不可用")
	}
	if exists && cacheFresh(cached.StoredAt, now, svc.settings.StationCacheTTL) {
		return cached.Data, cached.StoredAt, false, warnings
	}
	stations, err := svc.provider.fetchStations(ctx, apiKey, area, input.BrandName, svc.settings.StationLimit)
	if err != nil {
		if exists {
			warnings = append(warnings, "加油站目录请求失败，已返回最近缓存")
			return cached.Data, cached.StoredAt, true, warnings
		}
		warnings = append(warnings, err.Error())
		return nil, time.Time{}, false, warnings
	}
	if err := writeCache(ctx, actions, key, cacheEnvelope[[]station]{StoredAt: now, Data: stations}); err != nil {
		warnings = append(warnings, "本次加油站目录未能写入缓存")
	}
	return stations, now, false, warnings
}

func findRegionalPrice(prices []regionalPrice, area string) (regionalPrice, bool) {
	wanted := normalizeRegionName(area)
	for _, price := range prices {
		if normalizeRegionName(price.Region) == wanted {
			return price, true
		}
	}
	return regionalPrice{}, false
}

func readSecret(ctx context.Context, actions hostActions, key string) (string, error) {
	result, err := actions.SecretRead(ctx, key)
	if err != nil {
		return "", fmt.Errorf("读取插件 secret %s: %w", key, err)
	}
	if !boolValue(result["exists"]) {
		return "", nil
	}
	return strings.TrimSpace(stringValue(result["value"])), nil
}
