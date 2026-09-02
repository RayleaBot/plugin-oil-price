package plugin

import (
	"strings"
	"time"
)

const (
	defaultArea                = "北京"
	defaultStationLimit        = 5
	defaultStationRadiusKM     = 15
	defaultPriceCacheMinutes   = 60
	defaultStationCacheMinutes = 720
)

type settings struct {
	DefaultArea     string
	StationLimit    int
	StationRadiusKM int
	PriceCacheTTL   time.Duration
	StationCacheTTL time.Duration
	AreaCacheTTL    time.Duration
}

func settingsFromConfig(config map[string]any) settings {
	area := strings.TrimSpace(stringValue(config["default_area"]))
	if area == "" {
		area = defaultArea
	}
	stationLimit := clampInt(intValue(config["station_limit"]), 1, 10, defaultStationLimit)
	stationRadiusKM := clampInt(intValue(config["station_radius_km"]), 5, 30, defaultStationRadiusKM)
	priceMinutes := clampInt(intValue(config["price_cache_minutes"]), 5, 1440, defaultPriceCacheMinutes)
	stationMinutes := clampInt(intValue(config["station_cache_minutes"]), 10, 10080, defaultStationCacheMinutes)
	return settings{
		DefaultArea:     area,
		StationLimit:    stationLimit,
		StationRadiusKM: stationRadiusKM,
		PriceCacheTTL:   time.Duration(priceMinutes) * time.Minute,
		StationCacheTTL: time.Duration(stationMinutes) * time.Minute,
		AreaCacheTTL:    7 * 24 * time.Hour,
	}
}

func clampInt(value, minimum, maximum, fallback int) int {
	if value < minimum || value > maximum {
		return fallback
	}
	return value
}
