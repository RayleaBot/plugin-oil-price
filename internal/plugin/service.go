package plugin

import (
	"context"
	"fmt"
	"time"
)

type hostActions interface {
	cacheActions
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
	return &service{
		settings: config,
		provider: providerClient{http: doer, nominatimLimiter: publicNominatimLimiter},
		now:      now,
	}
}

func (svc *service) query(ctx context.Context, actions hostActions, input queryInput) (queryResult, error) {
	now := svc.now()
	result := queryResult{Area: areaResolution{Requested: input.Area}, StationRadiusKM: svc.settings.StationRadiusKM, QueriedAt: now}
	if !input.StationsOnly {
		bundle, storedAt, stale, warnings, err := svc.loadPriceBundle(ctx, actions, now)
		if err != nil {
			return queryResult{}, err
		}
		area, quote, err := resolveRegionalPrice(bundle, input.Area)
		if err != nil {
			return queryResult{}, err
		}
		result.Area = area
		result.Price = quote
		result.AdjustmentDate = bundle.AdjustmentDate
		result.EffectiveFrom = bundle.EffectiveFrom
		result.PriceStoredAt = storedAt
		result.PriceStale = stale
		result.Warnings = append(result.Warnings, warnings...)
	}
	stations, storedAt, stale, warnings := svc.loadStations(ctx, actions, input, now)
	result.Stations = stations
	result.StationStored = storedAt
	result.StationStale = stale
	result.Warnings = append(result.Warnings, warnings...)
	return result, nil
}

func (svc *service) loadPriceBundle(ctx context.Context, actions hostActions, now time.Time) (priceBundle, time.Time, bool, []string, error) {
	cached, exists, cacheErr := readCache[priceBundle](ctx, actions, priceCacheKey)
	warnings := make([]string, 0, 2)
	if cacheErr != nil {
		warnings = append(warnings, "地区油价缓存不可用")
	}
	if exists && cacheFresh(cached.StoredAt, now, svc.settings.PriceCacheTTL) {
		return cached.Data, cached.StoredAt, false, warnings, nil
	}
	bundle, fetchErr := svc.provider.fetchPriceBundle(ctx)
	if fetchErr != nil {
		if exists {
			warnings = append(warnings, "公开油价数据请求失败，已返回最近缓存")
			return cached.Data, cached.StoredAt, true, warnings, nil
		}
		return priceBundle{}, time.Time{}, false, warnings, fetchErr
	}
	if err := writeCache(ctx, actions, priceCacheKey, cacheEnvelope[priceBundle]{StoredAt: now, Data: bundle}); err != nil {
		warnings = append(warnings, "本次油价未能写入缓存")
	}
	return bundle, now, false, warnings, nil
}

func (svc *service) loadStationCenter(ctx context.Context, actions hostActions, requested string, now time.Time) (stationCenter, string, error) {
	key := cacheKey("geocode", normalizeText(requested))
	cached, exists, cacheErr := readCache[stationCenter](ctx, actions, key)
	warning := ""
	if cacheErr != nil {
		warning = "地区解析缓存不可用"
	}
	if exists && cacheFresh(cached.StoredAt, now, svc.settings.AreaCacheTTL) {
		return cached.Data, warning, nil
	}
	center, err := svc.provider.geocodeArea(ctx, requested)
	if err != nil {
		if exists {
			return cached.Data, "地区解析服务不可用，已使用最近缓存", nil
		}
		return stationCenter{}, warning, err
	}
	if err := writeCache(ctx, actions, key, cacheEnvelope[stationCenter]{StoredAt: now, Data: center}); err != nil {
		warning = "本次地区解析未能写入缓存"
	}
	return center, warning, nil
}

func (svc *service) loadStations(ctx context.Context, actions hostActions, input queryInput, now time.Time) ([]station, time.Time, bool, []string) {
	key := cacheKey(
		"station-osm-v1",
		normalizeText(input.Area),
		input.BrandID,
		fmt.Sprintf("%d", svc.settings.StationRadiusKM),
		fmt.Sprintf("%d", svc.settings.StationLimit),
	)
	cached, exists, cacheErr := readCache[[]station](ctx, actions, key)
	warnings := make([]string, 0, 3)
	if cacheErr != nil {
		warnings = append(warnings, "加油站缓存不可用")
	}
	if exists && cacheFresh(cached.StoredAt, now, svc.settings.StationCacheTTL) {
		return cached.Data, cached.StoredAt, false, warnings
	}
	center, areaWarning, err := svc.loadStationCenter(ctx, actions, input.Area, now)
	if areaWarning != "" {
		warnings = append(warnings, areaWarning)
	}
	if err != nil {
		if exists {
			warnings = append(warnings, "地区解析请求失败，已返回最近站点缓存")
			return cached.Data, cached.StoredAt, true, warnings
		}
		warnings = append(warnings, err.Error())
		return nil, time.Time{}, false, warnings
	}
	stations, err := svc.provider.fetchStations(ctx, center, input.BrandID, svc.settings.StationRadiusKM, svc.settings.StationLimit)
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
