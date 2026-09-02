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
	Region string            `json:"region"`
	Prices map[string]string `json:"prices"`
}

type areaResolution struct {
	Requested string `json:"requested"`
	Province  string `json:"province"`
	City      string `json:"city,omitempty"`
	District  string `json:"district,omitempty"`
	ADCode    string `json:"adcode,omitempty"`
	Location  string `json:"location,omitempty"`
}

type station struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BrandID  string `json:"brand_id"`
	Brand    string `json:"brand"`
	Province string `json:"province,omitempty"`
	City     string `json:"city,omitempty"`
	District string `json:"district,omitempty"`
	Address  string `json:"address,omitempty"`
	Location string `json:"location,omitempty"`
	Phone    string `json:"phone,omitempty"`
}

type queryResult struct {
	Area          areaResolution
	Price         regionalPrice
	PriceStoredAt time.Time
	PriceStale    bool
	Stations      []station
	StationStored time.Time
	StationStale  bool
	QueriedAt     time.Time
	Warnings      []string
}
