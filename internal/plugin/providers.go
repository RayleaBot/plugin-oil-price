package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	githubDataBaseURL = "https://raw.githubusercontent.com/luckkyboy/chinese-oil-price-data/main/data/"
	cdnDataBaseURL    = "https://cdn.jsdelivr.net/gh/luckkyboy/chinese-oil-price-data@main/data/"
	nominatimURL      = "https://nominatim.openstreetmap.org/search"
	overpassKumiURL   = "https://overpass.kumi.systems/api/interpreter"
	overpassMainURL   = "https://overpass-api.de/api/interpreter"
	providerUserAgent = "RayleaBot-OilPrice/0.2 (+https://github.com/RayleaBot/plugin-oil-price)"
	maxBodyBytes      = 2 << 20
	priceRequestLimit = 25 * time.Second
	geocodeLimit      = 10 * time.Second
	stationLimit      = 25 * time.Second
)

var priceDocumentPattern = regexp.MustCompile(`^\d{4}/\d{4}-\d{2}-\d{2}\.json$`)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type requestLimiter struct {
	mu          sync.Mutex
	lastRequest time.Time
	interval    time.Duration
}

var publicNominatimLimiter = &requestLimiter{interval: time.Second}

func (limiter *requestLimiter) wait(ctx context.Context) error {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	waitFor := limiter.interval - time.Since(limiter.lastRequest)
	if waitFor > 0 {
		timer := time.NewTimer(waitFor)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	limiter.lastRequest = time.Now()
	return nil
}

type providerClient struct {
	http             httpDoer
	nominatimLimiter *requestLimiter
}

func newProviderHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
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

func (client *providerClient) fetchPriceBundle(ctx context.Context) (priceBundle, error) {
	ctx, cancel := context.WithTimeout(ctx, priceRequestLimit)
	defer cancel()
	latestDocument, err := client.fetchPublicJSON(ctx, "prices/latest.json")
	if err != nil {
		return priceBundle{}, fmt.Errorf("公开油价索引不可用")
	}
	var latest struct {
		Latest         string `json:"latest"`
		AdjustmentDate string `json:"adjustment_date"`
		Status         string `json:"status"`
	}
	if err := json.Unmarshal(latestDocument, &latest); err != nil {
		return priceBundle{}, fmt.Errorf("解析公开油价索引: %w", err)
	}
	if latest.Status != "complete" || !priceDocumentPattern.MatchString(latest.Latest) {
		return priceBundle{}, fmt.Errorf("公开油价索引尚未生成完整数据")
	}
	priceDocument, err := client.fetchPublicJSON(ctx, "prices/"+latest.Latest)
	if err != nil {
		return priceBundle{}, fmt.Errorf("公开油价数据不可用")
	}
	regionDocument, err := client.fetchPublicJSON(ctx, "regions/regions.json")
	if err != nil {
		return priceBundle{}, fmt.Errorf("公开地区映射不可用")
	}
	bundle, err := parsePublicPriceBundle(priceDocument, regionDocument)
	if err != nil {
		return priceBundle{}, err
	}
	if bundle.AdjustmentDate != latest.AdjustmentDate {
		return priceBundle{}, fmt.Errorf("公开油价索引与数据日期不一致")
	}
	return bundle, nil
}

func (client *providerClient) fetchPublicJSON(ctx context.Context, relativePath string) ([]byte, error) {
	var lastErr error
	for _, baseURL := range []string{githubDataBaseURL, cdnDataBaseURL} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+relativePath, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", providerUserAgent)
		document, err := client.doJSON(req)
		if err == nil {
			return document, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func parsePublicPriceBundle(priceDocument, regionDocument []byte) (priceBundle, error) {
	var prices struct {
		AdjustmentDate string `json:"adjustment_date"`
		EffectiveFrom  string `json:"effective_from"`
		Unit           string `json:"unit"`
		Provinces      []struct {
			ProvinceCode string `json:"province_code"`
			ProvinceName string `json:"province_name"`
			Sources      []struct {
				URL         string `json:"url"`
				PublishedAt string `json:"published_at"`
				Confidence  string `json:"confidence"`
			} `json:"sources"`
			Zones []struct {
				ZoneCode string             `json:"zone_code"`
				ZoneName string             `json:"zone_name"`
				Items    map[string]float64 `json:"items"`
			} `json:"zones"`
		} `json:"provinces"`
	}
	if err := json.Unmarshal(priceDocument, &prices); err != nil {
		return priceBundle{}, fmt.Errorf("解析公开油价数据: %w", err)
	}
	if prices.AdjustmentDate == "" || prices.EffectiveFrom == "" || prices.Unit != "CNY/L" {
		return priceBundle{}, fmt.Errorf("公开油价数据缺少必要元数据")
	}
	bundle := priceBundle{AdjustmentDate: prices.AdjustmentDate, EffectiveFrom: prices.EffectiveFrom}
	for _, province := range prices.Provinces {
		if province.ProvinceCode == "" || strings.TrimSpace(province.ProvinceName) == "" {
			continue
		}
		var sourceURL, sourceDate, confidence string
		if len(province.Sources) > 0 {
			sourceURL = strings.TrimSpace(province.Sources[0].URL)
			sourceDate = strings.TrimSpace(province.Sources[0].PublishedAt)
			confidence = strings.TrimSpace(province.Sources[0].Confidence)
		}
		for _, zone := range province.Zones {
			entry := regionalPrice{
				ProvinceCode: province.ProvinceCode,
				Region:       strings.TrimSpace(province.ProvinceName),
				ZoneCode:     strings.TrimSpace(zone.ZoneCode),
				ZoneName:     strings.TrimSpace(zone.ZoneName),
				Prices:       make(map[string]string, 4),
				SourceURL:    sourceURL,
				SourceDate:   sourceDate,
				Confidence:   confidence,
			}
			for _, grade := range []string{"89", "92", "95", "98", "0"} {
				if value, ok := zone.Items[grade]; ok && value > 0 && value < 100 {
					entry.Prices[grade] = strconv.FormatFloat(value, 'f', 2, 64)
				}
			}
			if entry.ZoneCode != "" && len(entry.Prices) > 0 {
				bundle.Prices = append(bundle.Prices, entry)
			}
		}
	}
	var regions struct {
		Items []regionMapping `json:"items"`
	}
	if err := json.Unmarshal(regionDocument, &regions); err != nil {
		return priceBundle{}, fmt.Errorf("解析公开地区映射: %w", err)
	}
	for _, item := range regions.Items {
		if strings.TrimSpace(item.Region) != "" && item.ProvinceCode != "" && item.ZoneCode != "" {
			bundle.Regions = append(bundle.Regions, item)
		}
	}
	if len(bundle.Prices) == 0 || len(bundle.Regions) == 0 {
		return priceBundle{}, fmt.Errorf("公开油价数据未返回可用地区")
	}
	return bundle, nil
}

func resolveRegionalPrice(bundle priceBundle, requested string) (areaResolution, regionalPrice, error) {
	wanted := normalizeRegionLookup(requested)
	if wanted == "" {
		return areaResolution{}, regionalPrice{}, fmt.Errorf("请指定省、市或区县")
	}
	candidates := make([]regionMapping, 0, 4)
	for _, item := range bundle.Regions {
		region := normalizeRegionLookup(item.Region)
		locality := normalizeRegionLookup(item.Locality)
		if region == wanted || locality == wanted || strings.HasSuffix(region, wanted) {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		for _, price := range bundle.Prices {
			if normalizeRegionLookup(price.Region) == wanted {
				return areaResolution{Requested: requested, Province: price.Region}, price, nil
			}
		}
		return areaResolution{}, regionalPrice{}, fmt.Errorf("未找到地区“%s”的公开油价映射", requested)
	}
	selected := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.ProvinceCode != selected.ProvinceCode || candidate.ZoneCode != selected.ZoneCode {
			return areaResolution{}, regionalPrice{}, fmt.Errorf("地区“%s”存在多个匹配，请补充省市名称", requested)
		}
	}
	for _, price := range bundle.Prices {
		if price.ProvinceCode == selected.ProvinceCode && price.ZoneCode == selected.ZoneCode {
			return areaResolution{Requested: requested, Province: price.Region, City: selected.Locality}, price, nil
		}
	}
	return areaResolution{}, regionalPrice{}, fmt.Errorf("地区“%s”对应价区暂无油价", requested)
}

func (client *providerClient) geocodeArea(ctx context.Context, requested string) (stationCenter, error) {
	ctx, cancel := context.WithTimeout(ctx, geocodeLimit)
	defer cancel()
	if client.nominatimLimiter != nil {
		if err := client.nominatimLimiter.wait(ctx); err != nil {
			return stationCenter{}, fmt.Errorf("等待地区解析请求: %w", err)
		}
	}
	values := url.Values{
		"q":            {requested},
		"format":       {"jsonv2"},
		"countrycodes": {"cn"},
		"limit":        {"1"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nominatimURL+"?"+values.Encode(), nil)
	if err != nil {
		return stationCenter{}, fmt.Errorf("创建地区解析请求: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", providerUserAgent)
	document, err := client.doJSON(req)
	if err != nil {
		return stationCenter{}, fmt.Errorf("OpenStreetMap 地区解析服务不可用")
	}
	return parseNominatimArea(document, requested)
}

func parseNominatimArea(document []byte, requested string) (stationCenter, error) {
	var rows []struct {
		DisplayName string `json:"display_name"`
		Latitude    string `json:"lat"`
		Longitude   string `json:"lon"`
	}
	if err := json.Unmarshal(document, &rows); err != nil {
		return stationCenter{}, fmt.Errorf("解析 OpenStreetMap 地区信息: %w", err)
	}
	if len(rows) == 0 {
		return stationCenter{}, fmt.Errorf("OpenStreetMap 未识别地区“%s”", requested)
	}
	latitude, latErr := strconv.ParseFloat(rows[0].Latitude, 64)
	longitude, lonErr := strconv.ParseFloat(rows[0].Longitude, 64)
	if latErr != nil || lonErr != nil || latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
		return stationCenter{}, fmt.Errorf("OpenStreetMap 返回了无效坐标")
	}
	return stationCenter{Requested: requested, DisplayName: rows[0].DisplayName, Latitude: latitude, Longitude: longitude}, nil
}

func (client *providerClient) fetchStations(ctx context.Context, center stationCenter, brandID string, radiusKM, limit int) ([]station, error) {
	ctx, cancel := context.WithTimeout(ctx, stationLimit)
	defer cancel()
	query := fmt.Sprintf(`[out:json][timeout:15];nwr["amenity"="fuel"](around:%d,%.6f,%.6f);out center tags 50;`, radiusKM*1000, center.Latitude, center.Longitude)
	form := url.Values{"data": {query}}.Encode()
	var lastErr error
	for _, endpoint := range []string{overpassKumiURL, overpassMainURL} {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(form))
		if err != nil {
			return nil, fmt.Errorf("创建加油站目录请求: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", providerUserAgent)
		document, err := client.doJSON(req)
		if err != nil {
			lastErr = err
			continue
		}
		stations, err := parseOSMStations(document, center, brandID)
		if err != nil {
			lastErr = err
			continue
		}
		if len(stations) > limit {
			stations = stations[:limit]
		}
		return stations, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("OpenStreetMap 加油站目录服务不可用")
	}
	return nil, fmt.Errorf("OpenStreetMap 加油站目录未返回数据")
}

func parseOSMStations(document []byte, center stationCenter, brandID string) ([]station, error) {
	var response struct {
		Elements []struct {
			Type   string  `json:"type"`
			ID     int64   `json:"id"`
			Lat    float64 `json:"lat"`
			Lon    float64 `json:"lon"`
			Center struct {
				Lat float64 `json:"lat"`
				Lon float64 `json:"lon"`
			} `json:"center"`
			Tags map[string]string `json:"tags"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(document, &response); err != nil {
		return nil, fmt.Errorf("解析 OpenStreetMap 加油站目录: %w", err)
	}
	stations := make([]station, 0, len(response.Elements))
	for _, element := range response.Elements {
		if element.Tags["amenity"] != "fuel" {
			continue
		}
		name := firstNonEmpty(element.Tags["name:zh"], element.Tags["name"], element.Tags["brand:zh"], element.Tags["brand"], element.Tags["operator"])
		if name == "" {
			continue
		}
		brand := detectBrand(strings.Join([]string{name, element.Tags["brand"], element.Tags["operator"]}, " "), "")
		if brandID != "" && brand.ID != brandID {
			continue
		}
		latitude, longitude := element.Lat, element.Lon
		if latitude == 0 && longitude == 0 {
			latitude, longitude = element.Center.Lat, element.Center.Lon
		}
		address := firstNonEmpty(element.Tags["addr:full"], strings.TrimSpace(strings.Join(nonEmptyStrings(element.Tags["addr:street"], element.Tags["addr:housenumber"]), "")))
		stations = append(stations, station{
			ID:       fmt.Sprintf("%s/%d", element.Type, element.ID),
			Name:     name,
			BrandID:  brand.ID,
			Brand:    brand.Name,
			Province: element.Tags["addr:province"],
			City:     element.Tags["addr:city"],
			District: firstNonEmpty(element.Tags["addr:district"], element.Tags["addr:suburb"]),
			Address:  address,
			Location: fmt.Sprintf("%.6f,%.6f", longitude, latitude),
			Phone:    firstNonEmpty(element.Tags["contact:phone"], element.Tags["phone"]),
			Distance: haversineKM(center.Latitude, center.Longitude, latitude, longitude),
		})
	}
	sort.SliceStable(stations, func(i, j int) bool {
		leftRank, rightRank := brandRank(stations[i].BrandID), brandRank(stations[j].BrandID)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if stations[i].Distance != stations[j].Distance {
			return stations[i].Distance < stations[j].Distance
		}
		return stations[i].Name < stations[j].Name
	})
	return stations, nil
}

func haversineKM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKM = 6371.0
	toRadians := math.Pi / 180
	dLat := (lat2 - lat1) * toRadians
	dLon := (lon2 - lon1) * toRadians
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*toRadians)*math.Cos(lat2*toRadians)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func (client *providerClient) doJSON(req *http.Request) ([]byte, error) {
	resp, err := client.http.Do(req)
	if err != nil {
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
