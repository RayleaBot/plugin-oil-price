package plugin

import (
	"fmt"
	"strings"
)

var gradeLabels = map[string]string{
	"92": "92号汽油",
	"95": "95号汽油",
	"98": "98号汽油",
	"0":  "0号柴油",
}

func formatQueryResult(input queryInput, result queryResult) string {
	var out strings.Builder
	displayArea := strings.TrimSpace(result.Area.Requested)
	if displayArea == "" {
		displayArea = result.Price.Region
	}
	fmt.Fprintf(&out, "%s油价\n", displayArea)
	fmt.Fprintf(&out, "地区参考：%s（省级当前参考价）\n", result.Price.Region)
	grades := []string{"92", "95", "98", "0"}
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
	priceStatus := "查询"
	if result.PriceStale {
		priceStatus = "缓存"
	}
	fmt.Fprintf(&out, "参考价来源：聚合数据；%s时间：%s\n", priceStatus, result.PriceStoredAt.Format("2006-01-02 15:04"))

	out.WriteString("\n大型品牌加油站（目录信息，不代表站点挂牌价）\n")
	if len(result.Stations) == 0 {
		out.WriteString("暂无可展示的站点\n")
	} else {
		for index, item := range result.Stations {
			fmt.Fprintf(&out, "%d. [%s] %s\n", index+1, item.Brand, item.Name)
			location := strings.TrimSpace(strings.Join(nonEmptyStrings(item.District, item.Address), " · "))
			if location != "" {
				fmt.Fprintf(&out, "   %s\n", location)
			}
		}
		stationStatus := "查询"
		if result.StationStale {
			stationStatus = "缓存"
		}
		fmt.Fprintf(&out, "站点来源：高德地图；%s时间：%s\n", stationStatus, result.StationStored.Format("2006-01-02 15:04"))
	}
	if len(result.Warnings) > 0 {
		out.WriteString("\n提示：")
		out.WriteString(strings.Join(uniqueStrings(result.Warnings), "；"))
		out.WriteByte('\n')
	}
	out.WriteString("站点会员价、优惠价和挂牌价可能不同，请以现场公示为准。")
	return out.String()
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
