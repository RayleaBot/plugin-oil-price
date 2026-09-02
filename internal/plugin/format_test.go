package plugin

import (
	"strings"
	"testing"
	"time"
)

func TestFormatQueryResultSeparatesReferencePriceFromStationDirectory(t *testing.T) {
	result := queryResult{
		Area:          areaResolution{Requested: "深圳南山", Province: "广东省", City: "深圳市", District: "南山区"},
		Price:         regionalPrice{Region: "广东", Prices: map[string]string{"95": "8.07"}},
		PriceStoredAt: time.Date(2026, 9, 3, 12, 0, 0, 0, time.Local),
		Stations:      []station{{Name: "中国石化南山加油站", Brand: "中国石化", District: "南山区", Address: "示例路1号"}},
		StationStored: time.Date(2026, 9, 3, 12, 1, 0, 0, time.Local),
	}
	text := formatQueryResult(queryInput{Area: "深圳南山", Grade: "95"}, result)
	for _, want := range []string{"省级当前参考价", "95号汽油：8.07 元/升", "目录信息，不代表站点挂牌价", "[中国石化] 中国石化南山加油站", "以现场公示为准"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}

func TestFormatQueryResultDeduplicatesWarningsAndMarksStaleData(t *testing.T) {
	result := queryResult{
		Area:          areaResolution{Requested: "广东"},
		Price:         regionalPrice{Region: "广东", Prices: map[string]string{"92": "7.45"}},
		PriceStoredAt: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC),
		PriceStale:    true,
		Warnings:      []string{"上游不可用", "上游不可用", ""},
	}
	text := formatQueryResult(queryInput{Area: "广东", Grade: "92"}, result)
	if !strings.Contains(text, "缓存时间") || strings.Count(text, "上游不可用") != 1 {
		t.Fatalf("unexpected stale output:\n%s", text)
	}
}
