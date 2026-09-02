package plugin

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (fn doerFunc) Do(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonHTTPResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

const priceFixture = `{
  "adjustment_date":"2026-08-14",
  "effective_from":"2026-08-15T00:00:00+08:00",
  "unit":"CNY/L",
  "provinces":[{
    "province_code":"440000",
    "province_name":"广东省",
    "sources":[{"url":"https://drc.gd.gov.cn/example","published_at":"2026-08-14","confidence":"high"}],
    "zones":[
      {"zone_code":"default","zone_name":"默认价区","items":{"92":7.45,"95":8.07,"0":7.09}},
      {"zone_code":"island","zone_name":"海岛价区","items":{"92":7.55,"95":8.17,"0":7.19}}
    ]
  }]
}`

const regionFixture = `{
  "items":[
    {"region":"广东","province_code":"440000","zone_code":"default"},
    {"region":"广东深圳","province_code":"440000","zone_code":"default","locality":"深圳"},
    {"region":"广东海岛","province_code":"440000","zone_code":"island","locality":"海岛"}
  ]
}`

func TestFetchPriceBundleUsesOpenDataAndParsesZones(t *testing.T) {
	requestCount := 0
	client := providerClient{http: doerFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		if req.Method != http.MethodGet || req.Header.Get("User-Agent") != providerUserAgent {
			t.Fatalf("unexpected request: %s %s headers=%v", req.Method, req.URL, req.Header)
		}
		switch {
		case strings.HasSuffix(req.URL.Path, "/prices/latest.json"):
			return jsonHTTPResponse(`{"latest":"2026/2026-08-14.json","adjustment_date":"2026-08-14","status":"complete"}`), nil
		case strings.HasSuffix(req.URL.Path, "/prices/2026/2026-08-14.json"):
			return jsonHTTPResponse(priceFixture), nil
		case strings.HasSuffix(req.URL.Path, "/regions/regions.json"):
			return jsonHTTPResponse(regionFixture), nil
		default:
			t.Fatalf("unexpected URL: %s", req.URL)
			return nil, errors.New("unexpected URL")
		}
	})}
	bundle, err := client.fetchPriceBundle(t.Context())
	if err != nil {
		t.Fatalf("fetchPriceBundle: %v", err)
	}
	if requestCount != 3 || len(bundle.Prices) != 2 || len(bundle.Regions) != 3 {
		t.Fatalf("bundle = %#v, requests = %d", bundle, requestCount)
	}
	if bundle.Prices[0].Prices["95"] != "8.07" || bundle.Prices[0].SourceURL == "" {
		t.Fatalf("price = %#v", bundle.Prices[0])
	}
}

func TestFetchPublicJSONFallsBackToCDN(t *testing.T) {
	var hosts []string
	client := providerClient{http: doerFunc(func(req *http.Request) (*http.Response, error) {
		hosts = append(hosts, req.URL.Hostname())
		if req.URL.Hostname() == "raw.githubusercontent.com" {
			return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}
		return jsonHTTPResponse(`{"ok":true}`), nil
	})}
	document, err := client.fetchPublicJSON(t.Context(), "prices/latest.json")
	if err != nil || string(document) != `{"ok":true}` {
		t.Fatalf("fetchPublicJSON = %s, %v", document, err)
	}
	if len(hosts) != 2 || hosts[1] != "cdn.jsdelivr.net" {
		t.Fatalf("hosts = %#v", hosts)
	}
}

func TestResolveRegionalPriceUsesCityZoneAndRejectsAmbiguity(t *testing.T) {
	bundle, err := parsePublicPriceBundle([]byte(priceFixture), []byte(regionFixture))
	if err != nil {
		t.Fatalf("parsePublicPriceBundle: %v", err)
	}
	area, price, err := resolveRegionalPrice(bundle, "广东海岛")
	if err != nil {
		t.Fatalf("resolveRegionalPrice: %v", err)
	}
	if area.Province != "广东省" || price.ZoneCode != "island" || price.Prices["92"] != "7.55" {
		t.Fatalf("area=%#v price=%#v", area, price)
	}
	bundle.Regions = append(bundle.Regions, regionMapping{Region: "广西海岛", ProvinceCode: "450000", ZoneCode: "default", Locality: "海岛"})
	if _, _, err := resolveRegionalPrice(bundle, "海岛"); err == nil || !strings.Contains(err.Error(), "多个匹配") {
		t.Fatalf("ambiguous error = %v", err)
	}
}

func TestGeocodeAreaUsesNominatimWithoutKey(t *testing.T) {
	client := providerClient{http: doerFunc(func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()
		if req.URL.String() == "" || query.Get("q") != "深圳" || query.Get("countrycodes") != "cn" || query.Get("key") != "" {
			t.Fatalf("unexpected query: %s", req.URL)
		}
		return jsonHTTPResponse(`[{"display_name":"深圳市, 广东省, 中国","lat":"22.5445741","lon":"114.0545429"}]`), nil
	})}
	center, err := client.geocodeArea(t.Context(), "深圳")
	if err != nil {
		t.Fatalf("geocodeArea: %v", err)
	}
	if center.Latitude != 22.5445741 || center.Longitude != 114.0545429 {
		t.Fatalf("center = %#v", center)
	}
}

func TestFetchStationsPrioritizesKnownBrandsAndAppliesFilter(t *testing.T) {
	client := providerClient{http: doerFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != overpassKumiURL {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil || !strings.Contains(values.Get("data"), `["amenity"="fuel"]`) || strings.Contains(values.Get("data"), `\"`) {
			t.Fatalf("invalid Overpass query: %q, %v", values.Get("data"), err)
		}
		return jsonHTTPResponse(`{"elements":[
          {"type":"node","id":1,"lat":22.55,"lon":114.06,"tags":{"amenity":"fuel","name":"社区能源站"}},
          {"type":"node","id":2,"lat":22.56,"lon":114.07,"tags":{"amenity":"fuel","name":"中国石化测试站","brand":"Sinopec","addr:district":"南山区"}},
          {"type":"way","id":3,"center":{"lat":22.57,"lon":114.08},"tags":{"amenity":"fuel","name":"中国石油测试站","operator":"PetroChina"}}
        ]}`), nil
	})}
	center := stationCenter{Latitude: 22.5445741, Longitude: 114.0545429}
	stations, err := client.fetchStations(t.Context(), center, "sinopec", 15, 5)
	if err != nil {
		t.Fatalf("fetchStations: %v", err)
	}
	if len(stations) != 1 || stations[0].BrandID != "sinopec" || stations[0].Distance <= 0 {
		t.Fatalf("stations = %#v", stations)
	}
}

func TestProviderResponseGuards(t *testing.T) {
	tests := []struct {
		name     string
		response *http.Response
	}{
		{name: "http error", response: &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader(`{"error":"upstream"}`))}},
		{name: "invalid json", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("not-json"))}},
		{name: "oversized", response: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxBodyBytes+1)))}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := providerClient{http: doerFunc(func(*http.Request) (*http.Response, error) { return test.response, nil })}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/data", nil)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			if _, err := client.doJSON(req); err == nil {
				t.Fatal("expected response guard error")
			}
		})
	}
}

func TestProviderClientRejectsUnsafeRedirects(t *testing.T) {
	client := newProviderHTTPClient()
	original := &http.Request{URL: &url.URL{Scheme: "https", Host: "nominatim.openstreetmap.org", Path: "/search"}}
	crossHost := &http.Request{URL: &url.URL{Scheme: "https", Host: "example.test", Path: "/redirect"}}
	if err := client.CheckRedirect(crossHost, []*http.Request{original}); err == nil {
		t.Fatal("expected cross-host redirect rejection")
	}
	if err := client.CheckRedirect(original, []*http.Request{original, original, original}); err == nil {
		t.Fatal("expected redirect limit rejection")
	}
	if err := client.CheckRedirect(original, []*http.Request{original}); err != nil {
		t.Fatalf("same-host redirect rejected: %v", err)
	}
}

func TestProviderRequestErrorDoesNotExposeURL(t *testing.T) {
	client := providerClient{http: doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("Get https://example.test/?private=sensitive-fixture: offline")
	})}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/?private=sensitive-fixture", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	_, err = client.doJSON(req)
	if err == nil || strings.Contains(err.Error(), "sensitive-fixture") {
		t.Fatalf("unsafe error = %v", err)
	}
}

func TestFetchPriceBundleRejectsUntrustedLatestPath(t *testing.T) {
	client := providerClient{http: doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonHTTPResponse(`{"latest":"../../secret.json","adjustment_date":"2026-08-14","status":"complete"}`), nil
	})}
	if _, err := client.fetchPriceBundle(t.Context()); err == nil || !strings.Contains(err.Error(), "尚未生成完整数据") {
		t.Fatalf("error = %v", err)
	}
}
