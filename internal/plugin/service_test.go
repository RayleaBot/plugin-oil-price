package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type fakeActions struct {
	mu      sync.Mutex
	secrets map[string]string
	kv      map[string]any
}

func newFakeActions() *fakeActions {
	return &fakeActions{secrets: map[string]string{}, kv: map[string]any{}}
}

func (fake *fakeActions) SecretRead(_ context.Context, key string) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	value, exists := fake.secrets[key]
	if !exists {
		return rayleabot.ActionResult{"key": key, "exists": false}, nil
	}
	return rayleabot.ActionResult{"key": key, "exists": true, "value": value}, nil
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

func storePriceFixture(t *testing.T, fake *fakeActions, storedAt time.Time) {
	t.Helper()
	raw, err := json.Marshal(cacheEnvelope[[]regionalPrice]{StoredAt: storedAt, Data: []regionalPrice{{Region: "广东", Prices: map[string]string{"92": "7.45", "95": "8.07", "0": "7.09"}}}})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	fake.kv[priceCacheKey] = string(raw)
}

func TestServiceUsesFreshPriceCacheWithoutCallingProvider(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	fake := newFakeActions()
	storePriceFixture(t, fake, now.Add(-10*time.Minute))
	doer := doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("provider must not be called")
	})
	result, err := newService(settingsFromConfig(nil), doer, func() time.Time { return now }).query(t.Context(), fake, queryInput{Area: "广东", Grade: "95"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if result.Price.Prices["95"] != "8.07" || result.PriceStale {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Stations) != 0 {
		t.Fatalf("unexpected stations: %#v", result.Stations)
	}
}

func TestServiceFallsBackToExpiredPriceCacheWhenSecretMissing(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	fake := newFakeActions()
	storePriceFixture(t, fake, now.Add(-2*time.Hour))
	result, err := newService(settingsFromConfig(nil), doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("not called")
	}), func() time.Time { return now }).query(t.Context(), fake, queryInput{Area: "广东"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !result.PriceStale {
		t.Fatalf("expected stale result: %#v", result)
	}
}

func TestServiceFetchesCityPriceAndBrandStationsThenCachesSnapshots(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	fake := newFakeActions()
	fake.secrets[juheSecretKey] = "fixture-juhe"
	fake.secrets[amapSecretKey] = "fixture-amap"
	doer := doerFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/cnoil/oil_city":
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read price request: %v", err)
			}
			if !strings.Contains(string(body), "key=fixture-juhe") {
				t.Fatalf("price request did not contain form key: %s", body)
			}
			return jsonHTTPResponse(`{"reason":"success","result":[{"city":"广东","92h":"7.45","95h":"8.07","98h":"9.21","0h":"7.09"}],"error_code":0}`), nil
		case "/v3/geocode/geo":
			if req.URL.Query().Get("address") != "深圳" || req.URL.Query().Get("key") != "fixture-amap" {
				t.Fatalf("unexpected geocode query: %#v", req.URL.Query())
			}
			return jsonHTTPResponse(`{"status":"1","infocode":"10000","geocodes":[{"province":"广东省","city":"深圳市","district":[],"adcode":"440300","location":"114.05,22.55"}]}`), nil
		case "/v5/place/text":
			if req.URL.Query().Get("region") != "440300" || req.URL.Query().Get("keywords") != "中国石化" {
				t.Fatalf("unexpected station query: %#v", req.URL.Query())
			}
			return jsonHTTPResponse(`{"status":"1","infocode":"10000","pois":[{"id":"s1","name":"中国石化南山加油站","type":"汽车服务;加油站","pname":"广东省","cityname":"深圳市","adname":"南山区","address":"示例路1号","location":"113.1,22.5","business":{"tel":"0755-00000000"}},{"id":"p1","name":"中国石油福田加油站","type":"汽车服务;加油站","pname":"广东省","cityname":"深圳市","adname":"福田区","address":"示例路2号","location":"114.1,22.5","business":{"tel":"0755-11111111"}}]}`), nil
		default:
			t.Fatalf("unexpected provider path: %s", req.URL.Path)
			return nil, errors.New("unexpected provider path")
		}
	})

	result, err := newService(settingsFromConfig(map[string]any{"station_limit": 3}), doer, func() time.Time { return now }).query(
		t.Context(), fake, queryInput{Area: "深圳", Grade: "95", BrandID: "sinopec", BrandName: "中国石化"},
	)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if result.Price.Region != "广东" || result.Price.Prices["95"] != "8.07" {
		t.Fatalf("price result = %#v", result.Price)
	}
	if result.Area.ADCode != "440300" || result.Area.City != "深圳市" {
		t.Fatalf("area result = %#v", result.Area)
	}
	if len(result.Stations) != 1 || result.Stations[0].BrandID != "sinopec" {
		t.Fatalf("station result = %#v", result.Stations)
	}
	if len(fake.kv) != 3 {
		t.Fatalf("cache entries = %d, want 3", len(fake.kv))
	}
}

func TestServiceFallsBackToExpiredAreaAndStationCaches(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	fake := newFakeActions()
	fake.secrets[amapSecretKey] = "fixture-amap"
	storePriceFixture(t, fake, now.Add(-10*time.Minute))
	area := areaResolution{Requested: "深圳", Province: "广东省", City: "深圳市", ADCode: "440300"}
	areaRaw, err := json.Marshal(cacheEnvelope[areaResolution]{StoredAt: now.Add(-8 * 24 * time.Hour), Data: area})
	if err != nil {
		t.Fatalf("marshal area fixture: %v", err)
	}
	fake.kv[cacheKey("area", normalizeText("深圳"))] = string(areaRaw)
	stationData := []station{{ID: "s1", Name: "中国石化南山加油站", BrandID: "sinopec", Brand: "中国石化"}}
	stationRaw, err := json.Marshal(cacheEnvelope[[]station]{StoredAt: now.Add(-13 * time.Hour), Data: stationData})
	if err != nil {
		t.Fatalf("marshal station fixture: %v", err)
	}
	fake.kv[cacheKey("station", normalizeText("深圳"), "", "5")] = string(stationRaw)

	result, err := newService(settingsFromConfig(nil), doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("provider offline")
	}), func() time.Time { return now }).query(t.Context(), fake, queryInput{Area: "深圳"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !result.StationStale || len(result.Stations) != 1 || result.Area.ADCode != "440300" {
		t.Fatalf("fallback result = %#v", result)
	}
	joined := strings.Join(result.Warnings, "|")
	if !strings.Contains(joined, "地区解析服务不可用") || !strings.Contains(joined, "加油站目录请求失败") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}
