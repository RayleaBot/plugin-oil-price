package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type fakeActions struct {
	mu sync.Mutex
	kv map[string]any
}

func newFakeActions() *fakeActions {
	return &fakeActions{kv: map[string]any{}}
}

func (fake *fakeActions) KVGet(_ context.Context, key string) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	value, exists := fake.kv[key]
	return rayleabot.ActionResult{"exists": exists, "value": value}, nil
}

func (fake *fakeActions) KVSet(_ context.Context, key string, value any) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.kv[key] = value
	return rayleabot.ActionResult{"ok": true}, nil
}

func testPriceBundle(t *testing.T) priceBundle {
	t.Helper()
	bundle, err := parsePublicPriceBundle([]byte(priceFixture), []byte(regionFixture))
	if err != nil {
		t.Fatalf("parse bundle fixture: %v", err)
	}
	return bundle
}

func storeCacheFixture[T any](t *testing.T, fake *fakeActions, key string, storedAt time.Time, data T) {
	t.Helper()
	raw, err := json.Marshal(cacheEnvelope[T]{StoredAt: storedAt, Data: data})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	fake.kv[key] = string(raw)
}

func stationCacheKey(input queryInput, settings settings) string {
	return cacheKey("station-osm-v1", normalizeText(input.Area), input.BrandID, fmt.Sprintf("%d", settings.StationRadiusKM), fmt.Sprintf("%d", settings.StationLimit))
}

func TestServiceUsesFreshCachesWithoutCallingProvider(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	fake := newFakeActions()
	input := queryInput{Area: "深圳", Grade: "95"}
	config := settingsFromConfig(nil)
	storeCacheFixture(t, fake, priceCacheKey, now.Add(-10*time.Minute), testPriceBundle(t))
	stations := []station{{ID: "node/1", Name: "中国石化测试站", BrandID: "sinopec", Brand: "中国石化"}}
	storeCacheFixture(t, fake, stationCacheKey(input, config), now.Add(-10*time.Minute), stations)
	svc := newService(config, doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("provider must not be called")
	}), func() time.Time { return now })
	svc.provider.nominatimLimiter = nil
	result, err := svc.query(t.Context(), fake, input)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if result.Price.Prices["95"] != "8.07" || result.PriceStale || len(result.Stations) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestServiceFallsBackToExpiredPriceCache(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	fake := newFakeActions()
	input := queryInput{Area: "广东"}
	config := settingsFromConfig(nil)
	storeCacheFixture(t, fake, priceCacheKey, now.Add(-2*time.Hour), testPriceBundle(t))
	storeCacheFixture(t, fake, stationCacheKey(input, config), now.Add(-10*time.Minute), []station{})
	svc := newService(config, doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("provider offline")
	}), func() time.Time { return now })
	svc.provider.nominatimLimiter = nil
	result, err := svc.query(t.Context(), fake, input)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !result.PriceStale || !strings.Contains(strings.Join(result.Warnings, "|"), "最近缓存") {
		t.Fatalf("result = %#v", result)
	}
}

func TestServiceFetchesCityPriceAndBrandStationsThenCachesSnapshots(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	fake := newFakeActions()
	doer := doerFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/prices/latest.json"):
			return jsonHTTPResponse(`{"latest":"2026/2026-08-14.json","adjustment_date":"2026-08-14","status":"complete"}`), nil
		case strings.HasSuffix(req.URL.Path, "/prices/2026/2026-08-14.json"):
			return jsonHTTPResponse(priceFixture), nil
		case strings.HasSuffix(req.URL.Path, "/regions/regions.json"):
			return jsonHTTPResponse(regionFixture), nil
		case req.URL.Hostname() == "nominatim.openstreetmap.org":
			if req.URL.Query().Get("q") != "深圳" {
				t.Fatalf("unexpected geocode query: %#v", req.URL.Query())
			}
			return jsonHTTPResponse(`[{"display_name":"深圳市, 广东省, 中国","lat":"22.5445741","lon":"114.0545429"}]`), nil
		case req.URL.Hostname() == "overpass.kumi.systems":
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read station request: %v", err)
			}
			if !strings.Contains(string(body), "around%3A15000") {
				t.Fatalf("unexpected station request: %s", body)
			}
			return jsonHTTPResponse(`{"elements":[
              {"type":"node","id":1,"lat":22.55,"lon":114.06,"tags":{"amenity":"fuel","name":"中国石化南山加油站","brand":"Sinopec"}},
              {"type":"node","id":2,"lat":22.56,"lon":114.07,"tags":{"amenity":"fuel","name":"中国石油福田加油站","brand":"PetroChina"}}
            ]}`), nil
		default:
			t.Fatalf("unexpected provider URL: %s", req.URL)
			return nil, errors.New("unexpected provider URL")
		}
	})
	config := settingsFromConfig(map[string]any{"station_limit": "5"})
	svc := newService(config, doer, func() time.Time { return now })
	svc.provider.nominatimLimiter = nil
	result, err := svc.query(t.Context(), fake, queryInput{Area: "深圳", Grade: "95", BrandID: "sinopec", BrandName: "中国石化"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if result.Price.Region != "广东省" || result.Price.Prices["95"] != "8.07" {
		t.Fatalf("price result = %#v", result.Price)
	}
	if len(result.Stations) != 1 || result.Stations[0].BrandID != "sinopec" {
		t.Fatalf("station result = %#v", result.Stations)
	}
	if len(fake.kv) != 3 {
		t.Fatalf("cache entries = %d, want 3", len(fake.kv))
	}
}

func TestServiceFallsBackToExpiredGeocodeAndStationCaches(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	fake := newFakeActions()
	input := queryInput{Area: "深圳"}
	config := settingsFromConfig(nil)
	storeCacheFixture(t, fake, priceCacheKey, now.Add(-10*time.Minute), testPriceBundle(t))
	center := stationCenter{Requested: "深圳", DisplayName: "深圳市", Latitude: 22.54, Longitude: 114.05}
	storeCacheFixture(t, fake, cacheKey("geocode", normalizeText("深圳")), now.Add(-8*24*time.Hour), center)
	stationData := []station{{ID: "node/1", Name: "中国石化南山加油站", BrandID: "sinopec", Brand: "中国石化"}}
	storeCacheFixture(t, fake, stationCacheKey(input, config), now.Add(-13*time.Hour), stationData)
	svc := newService(config, doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("provider offline")
	}), func() time.Time { return now })
	svc.provider.nominatimLimiter = nil
	result, err := svc.query(t.Context(), fake, input)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !result.StationStale || len(result.Stations) != 1 {
		t.Fatalf("fallback result = %#v", result)
	}
	joined := strings.Join(result.Warnings, "|")
	if !strings.Contains(joined, "地区解析服务不可用") || !strings.Contains(joined, "加油站目录请求失败") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestStationsOnlyDoesNotRequestPriceData(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	fake := newFakeActions()
	input := queryInput{Area: "深圳", StationsOnly: true}
	config := settingsFromConfig(nil)
	storeCacheFixture(t, fake, stationCacheKey(input, config), now.Add(-10*time.Minute), []station{})
	svc := newService(config, doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("provider must not be called")
	}), func() time.Time { return now })
	result, err := svc.query(t.Context(), fake, input)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !result.PriceStoredAt.IsZero() || result.AdjustmentDate != "" {
		t.Fatalf("unexpected price data: %#v", result)
	}
}
