package plugin

import (
	"fmt"
	"strings"
	"time"
)

var gradeLabels = map[string]string{
	"92": "92号汽油",
	"95": "95号汽油",
	"98": "98号汽油",
	"0":  "0号柴油",
}

var supportedGrades = [...]string{"92", "95", "98", "0"}

func formatSupportedBrands() string {
	var out strings.Builder
	out.WriteString("支持查询的油号与加油站品牌\n\n油号\n")
	for _, grade := range supportedGrades {
		fmt.Fprintf(&out, "- %s\n", gradeLabels[grade])
	}
	out.WriteString("\n加油站品牌\n")
	for _, brand := range knownBrands {
		fmt.Fprintf(&out, "- %s", brand.Name)
		if len(brand.Aliases) > 1 {
			fmt.Fprintf(&out, "（别名：%s）", strings.Join(brand.Aliases[1:], " / "))
		}
		out.WriteByte('\n')
	}
	out.WriteString("\n示例：/油价 深圳 95 中石化\n")
	out.WriteString("说明：部分价区不提供 98 号汽油；站点品牌目录来自 OpenStreetMap，覆盖可能不完整。")
	return out.String()
}

func formatQueryResult(input queryInput, result queryResult) string {
	var out strings.Builder
	displayArea := strings.TrimSpace(result.Area.Requested)
	if displayArea == "" {
		displayArea = input.Area
	}
	if !input.StationsOnly {
		fmt.Fprintf(&out, "%s油价\n", displayArea)
		zoneName := strings.TrimSpace(result.Price.ZoneName)
		if zoneName == "" {
			zoneName = "默认价区"
		}
		fmt.Fprintf(&out, "参考价区：%s · %s\n", result.Price.Region, zoneName)
		grades := supportedGrades[:]
		if input.Grade != "" {
			grades = []string{input.Grade}
		}
		for _, grade := range grades {
			price := strings.TrimSpace(result.Price.Prices[grade])
			if price == "" {
				price = "暂无"
			} else {
				price += " 元/升"
			}
			fmt.Fprintf(&out, "%s：%s\n", gradeLabels[grade], price)
		}
		fmt.Fprintf(&out, "调价日期：%s；生效时间：%s\n", result.AdjustmentDate, displayEffectiveTime(result.EffectiveFrom))
		priceStatus := "获取"
		if result.PriceStale {
			priceStatus = "缓存"
		}
		fmt.Fprintf(&out, "数据来源：chinese-oil-price-data（国家发改委及省级发改委公告汇集）；%s时间：%s\n", priceStatus, result.PriceStoredAt.Format("2006-01-02 15:04"))
		if result.Price.SourceURL != "" {
			fmt.Fprintf(&out, "本价区公告：%s\n", result.Price.SourceURL)
		}
		out.WriteByte('\n')
	}

	fmt.Fprintf(&out, "%s中心附近 %d 公里加油站（社区目录，不代表站点挂牌价）\n", displayArea, result.StationRadiusKM)
	if len(result.Stations) == 0 {
		out.WriteString("暂无可展示的站点\n")
	} else {
		for index, item := range result.Stations {
			fmt.Fprintf(&out, "%d. [%s] %s", index+1, item.Brand, item.Name)
			if item.Distance > 0 {
				fmt.Fprintf(&out, "（约 %.1f 公里）", item.Distance)
			}
			out.WriteByte('\n')
			location := strings.TrimSpace(strings.Join(nonEmptyStrings(item.District, item.Address), " · "))
			if location != "" {
				fmt.Fprintf(&out, "   %s\n", location)
			}
		}
		stationStatus := "获取"
		if result.StationStale {
			stationStatus = "缓存"
		}
		if !result.StationStored.IsZero() {
			fmt.Fprintf(&out, "站点%s时间：%s\n", stationStatus, result.StationStored.Format("2006-01-02 15:04"))
		}
	}
	out.WriteString("站点来源：© OpenStreetMap contributors（ODbL，openstreetmap.org/copyright）；覆盖可能不完整。\n")
	if len(result.Warnings) > 0 {
		out.WriteString("\n提示：")
		out.WriteString(strings.Join(uniqueStrings(result.Warnings), "；"))
		out.WriteByte('\n')
	}
	out.WriteString("油价为地区政府指导/最高零售价参考；会员价、优惠价和挂牌价请以站点现场公示为准。")
	return out.String()
}

func displayEffectiveTime(value string) string {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return value
	}
	return parsed.Format("2006-01-02 15:04")
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
