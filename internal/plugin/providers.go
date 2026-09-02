package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	juhePriceURL   = "https://apis.juhe.cn/cnoil/oil_city"
	amapGeocodeURL = "https://restapi.amap.com/v3/geocode/geo"
	amapStationURL = "https://restapi.amap.com/v5/place/text"
	maxBodyBytes   = 2 << 20
)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type providerClient struct {
	http httpDoer
}

func newProviderHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("provider redirect limit exceeded")
			}
			if req.URL.Scheme != "https" || len(via) == 0 || !strings.EqualFold(req.URL.Hostname(), via[0].URL.Hostname()) {
				return errors.New("provider redirect rejected")
			}
			return nil
		},
	}
}

func (client providerClient) fetchPrices(ctx context.Context, apiKey string) ([]regionalPrice, error) {
	body := url.Values{"key": {apiKey}, "dtype": {"json"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, juhePriceURL, bytes.NewBufferString(body))
	if err != nil {
		return nil, fmt.Errorf("创建油价请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	document, err := client.doJSON(req)
	if err != nil {
		return nil, fmt.Errorf("油价数据源不可用")
	}
	return parseJuhePrices(document)
}

func (client providerClient) resolveArea(ctx context.Context, apiKey, area string) (areaResolution, error) {
	values := url.Values{"key": {apiKey}, "address": {area}, "output": {"JSON"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, amapGeocodeURL+"?"+values.Encode(), nil)
	if err != nil {
		return areaResolution{}, fmt.Errorf("创建地区解析请求: %w", err)
	}
	document, err := client.doJSON(req)
	if err != nil {
		return areaResolution{}, fmt.Errorf("地区解析服务不可用")
	}
	return parseAmapArea(document, area)
}

func (client providerClient) fetchStations(ctx context.Context, apiKey string, area areaResolution, brandName string, limit int) ([]station, error) {
	keyword := "加油站"
	if strings.TrimSpace(brandName) != "" {
		keyword = brandName
	}
	region := strings.TrimSpace(area.ADCode)
	if region == "" {
		region = strings.TrimSpace(area.City)
	}
	if region == "" {
		region = strings.TrimSpace(area.Requested)
	}
	pageSize := limit * 3
	if pageSize < 10 {
		pageSize = 10
	}
	if pageSize > 25 {
		pageSize = 25
	}
	values := url.Values{
		"key":         {apiKey},
		"keywords":    {keyword},
		"region":      {region},
		"city_limit":  {"true"},
		"show_fields": {"business"},
		"page_size":   {strconv.Itoa(pageSize)},
		"page_num":    {"1"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, amapStationURL+"?"+values.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("创建加油站请求: %w", err)
	}
	document, err := client.doJSON(req)
	if err != nil {
		return nil, fmt.Errorf("加油站目录服务不可用")
	}
	stations, err := parseAmapStations(document)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(brandName) != "" {
		brand, _ := brandByAlias(brandName)
		filtered := stations[:0]
		for _, item := range stations {
			if item.BrandID == brand.ID {
				filtered = append(filtered, item)
			}
		}
		stations = filtered
	}
	sort.SliceStable(stations, func(i, j int) bool {
		left, right := brandRank(stations[i].BrandID), brandRank(stations[j].BrandID)
		if left != right {
			return left < right
		}
		return stations[i].Name < stations[j].Name
	})
	if len(stations) > limit {
		stations = stations[:limit]
	}
	return stations, nil
}

func (client providerClient) doJSON(req *http.Request) ([]byte, error) {
	resp, err := client.http.Do(req)
	if err != nil {
		// Do errors may contain the complete URL. Amap requires its key in the
		// query string, so the original error must not escape into logs or replies.
		return nil, errors.New("provider request failed")
	}
	limited := io.LimitReader(resp.Body, maxBodyBytes+1)
	body, readErr := io.ReadAll(limited)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, errors.New("provider response read failed")
	}
	if closeErr != nil {
		return nil, errors.New("provider response close failed")
	}
	if len(body) > maxBodyBytes {
		return nil, errors.New("provider response too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	if !json.Valid(body) {
		return nil, errors.New("provider returned invalid JSON")
	}
	return body, nil
}

func parseJuhePrices(document []byte) ([]regionalPrice, error) {
	var response struct {
		Reason    string          `json:"reason"`
		Result    json.RawMessage `json:"result"`
		ErrorCode int             `json:"error_code"`
	}
	if err := json.Unmarshal(document, &response); err != nil {
		return nil, fmt.Errorf("解析油价数据: %w", err)
	}
	if response.ErrorCode != 0 {
		return nil, fmt.Errorf("油价数据源返回错误码 %d", response.ErrorCode)
	}
	var rows []map[string]any
	if err := json.Unmarshal(response.Result, &rows); err != nil {
		return nil, fmt.Errorf("解析油价列表: %w", err)
	}
	prices := make([]regionalPrice, 0, len(rows))
	for _, row := range rows {
		region := strings.TrimSpace(stringValue(row["city"]))
		if region == "" {
			continue
		}
		entry := regionalPrice{Region: region, Prices: map[string]string{}}
		for grade, field := range map[string]string{"92": "92h", "95": "95h", "98": "98h", "0": "0h"} {
			if price := normalizePrice(stringValue(row[field])); price != "" {
				entry.Prices[grade] = price
			}
		}
		if len(entry.Prices) > 0 {
			prices = append(prices, entry)
		}
	}
	if len(prices) == 0 {
		return nil, fmt.Errorf("油价数据源未返回可用地区")
	}
	return prices, nil
}

func normalizePrice(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 || parsed > 100 {
		return ""
	}
	return value
}

func parseAmapArea(document []byte, requested string) (areaResolution, error) {
	var response struct {
		Status   string `json:"status"`
		InfoCode string `json:"infocode"`
		Geocodes []struct {
			Province json.RawMessage `json:"province"`
			City     json.RawMessage `json:"city"`
			District json.RawMessage `json:"district"`
			ADCode   string          `json:"adcode"`
			Location string          `json:"location"`
		} `json:"geocodes"`
	}
	if err := json.Unmarshal(document, &response); err != nil {
		return areaResolution{}, fmt.Errorf("解析地区信息: %w", err)
	}
	if response.Status != "1" {
		return areaResolution{}, fmt.Errorf("地区解析服务返回错误码 %s", response.InfoCode)
	}
	if len(response.Geocodes) == 0 {
		return areaResolution{}, fmt.Errorf("未识别地区“%s”", requested)
	}
	first := response.Geocodes[0]
	province := rawFlexibleString(first.Province)
	city := rawFlexibleString(first.City)
	if province == "" {
		return areaResolution{}, fmt.Errorf("地区“%s”缺少省级归属", requested)
	}
	if city == "" && strings.HasSuffix(province, "市") {
		city = province
	}
	return areaResolution{
		Requested: requested,
		Province:  province,
		City:      city,
		District:  rawFlexibleString(first.District),
		ADCode:    strings.TrimSpace(first.ADCode),
		Location:  strings.TrimSpace(first.Location),
	}, nil
}

func parseAmapStations(document []byte) ([]station, error) {
	var response struct {
		Status   string `json:"status"`
		InfoCode string `json:"infocode"`
		POIs     []struct {
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Type     string          `json:"type"`
			Province json.RawMessage `json:"pname"`
			City     json.RawMessage `json:"cityname"`
			District json.RawMessage `json:"adname"`
			Address  json.RawMessage `json:"address"`
			Location string          `json:"location"`
			Business struct {
				Phone json.RawMessage `json:"tel"`
			} `json:"business"`
		} `json:"pois"`
	}
	if err := json.Unmarshal(document, &response); err != nil {
		return nil, fmt.Errorf("解析加油站目录: %w", err)
	}
	if response.Status != "1" {
		return nil, fmt.Errorf("加油站目录服务返回错误码 %s", response.InfoCode)
	}
	stations := make([]station, 0, len(response.POIs))
	for _, poi := range response.POIs {
		name := strings.TrimSpace(poi.Name)
		if name == "" {
			continue
		}
		brand := detectBrand(name, poi.Type)
		stations = append(stations, station{
			ID:       strings.TrimSpace(poi.ID),
			Name:     name,
			BrandID:  brand.ID,
			Brand:    brand.Name,
			Province: rawFlexibleString(poi.Province),
			City:     rawFlexibleString(poi.City),
			District: rawFlexibleString(poi.District),
			Address:  rawFlexibleString(poi.Address),
			Location: strings.TrimSpace(poi.Location),
			Phone:    rawFlexibleString(poi.Business.Phone),
		})
	}
	return stations, nil
}

func rawFlexibleString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err == nil {
		return strings.TrimSpace(strings.Join(values, ","))
	}
	return ""
}
