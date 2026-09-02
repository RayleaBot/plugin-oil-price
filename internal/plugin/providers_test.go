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

func TestFetchPricesUsesPOSTAndParsesSupportedGrades(t *testing.T) {
	client := providerClient{http: doerFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != juhePriceURL {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if !strings.Contains(string(body), "key=fixture-key") || strings.Contains(req.URL.RawQuery, "fixture-key") {
			t.Fatalf("secret placement is unsafe: url=%s body=%s", req.URL, body)
		}
		return jsonHTTPResponse(`{"reason":"success","result":[{"city":"广东","92h":"7.45","95h":"8.07","98h":"9.21","0h":"7.09"},{"city":"测试","92h":"-"}],"error_code":0}`), nil
	})}
	prices, err := client.fetchPrices(t.Context(), "fixture-key")
	if err != nil {
		t.Fatalf("fetchPrices: %v", err)
	}
	if len(prices) != 1 || prices[0].Region != "广东" || prices[0].Prices["95"] != "8.07" {
		t.Fatalf("prices = %#v", prices)
	}
}

func TestParseAmapAreaHandlesMunicipalityArrayCity(t *testing.T) {
	area, err := parseAmapArea([]byte(`{"status":"1","infocode":"10000","geocodes":[{"province":"北京市","city":[],"district":"朝阳区","adcode":"110105","location":"116.48,39.99"}]}`), "北京朝阳")
	if err != nil {
		t.Fatalf("parseAmapArea: %v", err)
	}
	if area.Province != "北京市" || area.City != "北京市" || area.ADCode != "110105" {
		t.Fatalf("area = %#v", area)
	}
}

func TestParseAmapStationsNormalizesLargeBrands(t *testing.T) {
	stations, err := parseAmapStations([]byte(`{"status":"1","infocode":"10000","pois":[{"id":"p1","name":"中国石化南山加油站","type":"汽车服务;加油站","pname":"广东省","cityname":"深圳市","adname":"南山区","address":"示例路1号","location":"113.1,22.5","business":{"tel":"0755-00000000"}},{"id":"p2","name":"社区能源站","type":"汽车服务;加油站","pname":"广东省","cityname":"深圳市","adname":"福田区","address":[],"location":"113.2,22.6","business":{"tel":[]}}]}`))
	if err != nil {
		t.Fatalf("parseAmapStations: %v", err)
	}
	if len(stations) != 2 || stations[0].BrandID != "sinopec" || stations[1].BrandID != "other" {
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
			client := providerClient{http: doerFunc(func(*http.Request) (*http.Response, error) {
				return test.response, nil
			})}
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
	original := &http.Request{URL: &url.URL{Scheme: "https", Host: "restapi.amap.com", Path: "/v3/geocode/geo"}}
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
		return nil, errors.New("Get https://example.test/?key=sensitive-fixture: offline")
	})}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/?key=sensitive-fixture", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	_, err = client.doJSON(req)
	if err == nil || strings.Contains(err.Error(), "sensitive-fixture") {
		t.Fatalf("unsafe error = %v", err)
	}
}

func TestFetchStationsPrioritizesKnownBrandsAndAppliesLimit(t *testing.T) {
	client := providerClient{http: doerFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("region") != "440300" || req.URL.Query().Get("keywords") != "加油站" {
			t.Fatalf("unexpected query: %#v", req.URL.Query())
		}
		return jsonHTTPResponse(`{"status":"1","infocode":"10000","pois":[{"id":"o1","name":"社区能源站","type":"汽车服务;加油站"},{"id":"s1","name":"中国石化测试站","type":"汽车服务;加油站"},{"id":"p1","name":"中国石油测试站","type":"汽车服务;加油站"}]}`), nil
	})}
	stations, err := client.fetchStations(t.Context(), "fixture-amap", areaResolution{Requested: "深圳", ADCode: "440300"}, "", 2)
	if err != nil {
		t.Fatalf("fetchStations: %v", err)
	}
	if len(stations) != 2 || stations[0].BrandID != "petrochina" || stations[1].BrandID != "sinopec" {
		t.Fatalf("stations = %#v", stations)
	}
}

func TestProviderDocumentsRejectDeclaredFailures(t *testing.T) {
	if _, err := parseJuhePrices([]byte(`{"reason":"denied","result":[],"error_code":10001}`)); err == nil {
		t.Fatal("expected Juhe error code rejection")
	}
	if _, err := parseAmapArea([]byte(`{"status":"0","infocode":"10001","geocodes":[]}`), "深圳"); err == nil {
		t.Fatal("expected Amap geocode rejection")
	}
	if _, err := parseAmapStations([]byte(`{"status":"0","infocode":"10001","pois":[]}`)); err == nil {
		t.Fatal("expected Amap station rejection")
	}
}
