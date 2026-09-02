package plugin

import "time"

type queryInput struct {
	Area         string
	Grade        string
	BrandID      string
	BrandName    string
	StationsOnly bool
}

type regionalPrice struct {
	ProvinceCode string            `json:"province_code"`
	Region       string            `json:"region"`
	ZoneCode     string            `json:"zone_code"`
	ZoneName     string            `json:"zone_name"`
	Prices       map[string]string `json:"prices"`
	SourceURL    string            `json:"source_url,omitempty"`
	SourceDate   string            `json:"source_date,omitempty"`
	Confidence   string            `json:"confidence,omitempty"`
}

type regionMapping struct {
	Region       string `json:"region"`
	ProvinceCode string `json:"province_code"`
	ZoneCode     string `json:"zone_code"`
	Locality     string `json:"locality,omitempty"`
}

type priceBundle struct {
	AdjustmentDate string          `json:"adjustment_date"`
	EffectiveFrom  string          `json:"effective_from"`
	Prices         []regionalPrice `json:"prices"`
	Regions        []regionMapping `json:"regions"`
}

type areaResolution struct {
	Requested string `json:"requested"`
	Province  string `json:"province"`
	City      string `json:"city,omitempty"`
	District  string `json:"district,omitempty"`
	Location  string `json:"location,omitempty"`
}

type stationCenter struct {
	Requested   string  `json:"requested"`
	DisplayName string  `json:"display_name"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

type station struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	BrandID  string  `json:"brand_id"`
	Brand    string  `json:"brand"`
	Province string  `json:"province,omitempty"`
	City     string  `json:"city,omitempty"`
	District string  `json:"district,omitempty"`
	Address  string  `json:"address,omitempty"`
	Location string  `json:"location,omitempty"`
	Phone    string  `json:"phone,omitempty"`
	Distance float64 `json:"distance_km,omitempty"`
}

type queryResult struct {
	Area            areaResolution
	Price           regionalPrice
	AdjustmentDate  string
	EffectiveFrom   string
	PriceStoredAt   time.Time
	PriceStale      bool
	Stations        []station
	StationStored   time.Time
	StationStale    bool
	StationRadiusKM int
	QueriedAt       time.Time
	Warnings        []string
}
