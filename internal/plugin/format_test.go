package plugin

import (
	"strings"
	"testing"
	"time"
)

func TestFormatQueryResultSeparatesReferencePriceFromStationDirectory(t *testing.T) {
	result := queryResult{
		Area:            areaResolution{Requested: "深圳南山", Province: "广东省", City: "深圳市", District: "南山区"},
		Price:           regionalPrice{Region: "广东省", ZoneName: "默认价区", Prices: map[string]string{"95": "8.07"}, SourceURL: "https://drc.gd.gov.cn/example"},
		AdjustmentDate:  "2026-08-14",
		EffectiveFrom:   "2026-08-15T00:00:00+08:00",
		PriceStoredAt:   time.Date(2026, 9, 3, 12, 0, 0, 0, time.Local),
		Stations:        []station{{Name: "中国石化南山加油站", Brand: "中国石化", District: "南山区", Address: "示例路1号"}},
		StationStored:   time.Date(2026, 9, 3, 12, 1, 0, 0, time.Local),
		StationRadiusKM: 15,
	}
	text := formatQueryResult(queryInput{Area: "深圳南山", Grade: "95"}, result)
	for _, want := range []string{"参考价区：广东省 · 默认价区", "95号汽油：8.07 元/升", "中心附近 15 公里", "[中国石化] 中国石化南山加油站", "© OpenStreetMap contributors", "以站点现场公示为准"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}

func TestFormatQueryResultDeduplicatesWarningsAndMarksStaleData(t *testing.T) {
	result := queryResult{
		Area:            areaResolution{Requested: "广东"},
		Price:           regionalPrice{Region: "广东", Prices: map[string]string{"92": "7.45"}},
		PriceStoredAt:   time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC),
		PriceStale:      true,
		Warnings:        []string{"上游不可用", "上游不可用", ""},
		StationRadiusKM: 15,
	}
	text := formatQueryResult(queryInput{Area: "广东", Grade: "92"}, result)
	if !strings.Contains(text, "缓存时间") || strings.Count(text, "上游不可用") != 1 {
		t.Fatalf("unexpected stale output:\n%s", text)
	}
}
