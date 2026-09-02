package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSettingsFromConfigClampsValues(t *testing.T) {
	configured := settingsFromConfig(map[string]any{
		"default_area":          " 上海 ",
		"station_limit":         "8",
		"station_radius_km":     "25",
		"price_cache_minutes":   "120",
		"station_cache_minutes": "1440",
	})
	if configured.DefaultArea != "上海" || configured.StationLimit != 8 || configured.StationRadiusKM != 25 {
		t.Fatalf("configured = %#v", configured)
	}
	if configured.PriceCacheTTL != 2*time.Hour || configured.StationCacheTTL != 24*time.Hour {
		t.Fatalf("cache durations = %#v", configured)
	}
	fallback := settingsFromConfig(map[string]any{"station_limit": 99, "station_radius_km": 1})
	if fallback.StationLimit != defaultStationLimit || fallback.StationRadiusKM != defaultStationRadiusKM {
		t.Fatalf("fallback = %#v", fallback)
	}
}

func TestCacheFreshAndCacheKey(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	if !cacheFresh(now.Add(-time.Minute), now, 2*time.Minute) {
		t.Fatal("expected fresh cache")
	}
	if cacheFresh(now.Add(-3*time.Minute), now, 2*time.Minute) || cacheFresh(now.Add(10*time.Minute), now, 2*time.Minute) {
		t.Fatal("expected stale cache")
	}
	first := cacheKey("fixture", "a", "b")
	if first != cacheKey("fixture", "a", "b") || first == cacheKey("fixture", "ab") {
		t.Fatalf("cache keys are not stable: %q", first)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	fake := newFakeActions()
	want := cacheEnvelope[stationCenter]{
		StoredAt: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
		Data:     stationCenter{Requested: "深圳", Latitude: 22.54, Longitude: 114.05},
	}
	if err := writeCache(t.Context(), fake, "fixture", want); err != nil {
		t.Fatalf("writeCache: %v", err)
	}
	got, exists, err := readCache[stationCenter](t.Context(), fake, "fixture")
	if err != nil || !exists || got.Data.Requested != want.Data.Requested || !got.StoredAt.Equal(want.StoredAt) {
		t.Fatalf("readCache = %#v, %v, %v", got, exists, err)
	}
	if _, exists, err := readCache[stationCenter](t.Context(), fake, "missing"); err != nil || exists {
		t.Fatalf("missing cache = %v, %v", exists, err)
	}
}

func TestValueConversions(t *testing.T) {
	if stringValue(json.Number("12.5")) != "12.5" || stringValue(7) != "7" || stringValue(int64(8)) != "8" || stringValue(float64(1.25)) != "1.25" || stringValue(float32(1.5)) != "1.5" || stringValue(true) != "" {
		t.Fatal("unexpected string conversion")
	}
	if intValue(" 9 ") != 9 || intValue("bad") != 0 || !boolValue("true") || boolValue("no") {
		t.Fatal("unexpected scalar conversion")
	}
}

func TestBrandRankAndRequestLimiter(t *testing.T) {
	if brandRank("petrochina") >= brandRank("other") {
		t.Fatal("known brand should be prioritized")
	}
	limiter := &requestLimiter{}
	if err := limiter.wait(t.Context()); err != nil || limiter.lastRequest.IsZero() {
		t.Fatalf("wait = %v, last request = %v", err, limiter.lastRequest)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	limiter.interval = time.Hour
	if err := limiter.wait(canceled); err == nil {
		t.Fatal("expected canceled wait")
	}
}

func TestProviderParsersRejectInvalidDocuments(t *testing.T) {
	if _, err := parseNominatimArea([]byte(`[]`), "不存在"); err == nil {
		t.Fatal("expected empty geocode rejection")
	}
	if _, err := parseNominatimArea([]byte(`[{"lat":"bad","lon":"114"}]`), "深圳"); err == nil {
		t.Fatal("expected invalid coordinate rejection")
	}
	if _, err := parseOSMStations([]byte(`not-json`), stationCenter{}, ""); err == nil {
		t.Fatal("expected invalid station JSON rejection")
	}
	if _, err := parsePublicPriceBundle([]byte(`{}`), []byte(`{}`)); err == nil {
		t.Fatal("expected missing metadata rejection")
	}
}

func TestFormattingHelpers(t *testing.T) {
	if firstNonEmpty(" ", " value ") != "value" || firstNonEmpty("", " ") != "" {
		t.Fatal("unexpected firstNonEmpty result")
	}
	if haversineKM(22.54, 114.05, 22.55, 114.06) <= 0 {
		t.Fatal("expected positive distance")
	}
	if displayEffectiveTime("not-a-time") != "not-a-time" {
		t.Fatal("invalid time should be preserved")
	}
	values := uniqueStrings([]string{"提示", "提示", " "})
	if len(values) != 1 || !strings.Contains(strings.Join(values, "|"), "提示") {
		t.Fatalf("values = %#v", values)
	}
}

func TestNormalizeRegionLookupRemovesAdministrativeSuffixes(t *testing.T) {
	if got := normalizeRegionLookup(" 广东省 深圳市 南山区 "); got != "广东深圳南山" {
		t.Fatalf("normalizeRegionLookup = %q", got)
	}
}
