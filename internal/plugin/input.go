package plugin

import (
	"fmt"
	"strings"
)

type brandDefinition struct {
	ID      string
	Name    string
	Aliases []string
	Rank    int
}

var knownBrands = []brandDefinition{
	{ID: "petrochina", Name: "中国石油", Aliases: []string{"中国石油", "中石油", "petrochina", "中油"}, Rank: 1},
	{ID: "sinopec", Name: "中国石化", Aliases: []string{"中国石化", "中石化", "sinopec", "易捷"}, Rank: 2},
	{ID: "cnooc", Name: "中国海油", Aliases: []string{"中国海油", "中海油", "cnooc"}, Rank: 3},
	{ID: "shell", Name: "壳牌", Aliases: []string{"壳牌", "shell"}, Rank: 4},
	{ID: "yanchang", Name: "延长石油", Aliases: []string{"延长石油", "延长", "yan chang"}, Rank: 5},
}

func parseQueryInput(command string, args []string, fallbackArea string) (queryInput, error) {
	input := queryInput{StationsOnly: command == "油站"}
	areaParts := make([]string, 0, len(args))
	for _, raw := range args {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		if grade, ok := parseGrade(token); ok {
			if input.Grade != "" && input.Grade != grade {
				return queryInput{}, fmt.Errorf("一次只能查询一个油号")
			}
			input.Grade = grade
			continue
		}
		if brand, ok := brandByAlias(token); ok {
			if input.BrandID != "" && input.BrandID != brand.ID {
				return queryInput{}, fmt.Errorf("一次只能筛选一个品牌")
			}
			input.BrandID = brand.ID
			input.BrandName = brand.Name
			continue
		}
		areaParts = append(areaParts, token)
	}
	input.Area = strings.TrimSpace(strings.Join(areaParts, ""))
	if input.Area == "" {
		input.Area = strings.TrimSpace(fallbackArea)
	}
	if input.Area == "" {
		return queryInput{}, fmt.Errorf("请指定省、市或区县")
	}
	return input, nil
}

func parseGrade(raw string) (string, bool) {
	value := strings.NewReplacer("号", "", "#", "", "＃", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(raw)))
	if value == "柴油" || value == "0柴油" {
		return "0", true
	}
	value = strings.TrimSuffix(value, "汽油")
	switch value {
	case "92", "95", "98", "0":
		return value, true
	default:
		return "", false
	}
}

func brandByAlias(raw string) (brandDefinition, bool) {
	normalized := normalizeText(raw)
	for _, brand := range knownBrands {
		for _, alias := range brand.Aliases {
			if normalized == normalizeText(alias) {
				return brand, true
			}
		}
	}
	return brandDefinition{}, false
}

func detectBrand(name, category string) brandDefinition {
	haystack := normalizeText(name + " " + category)
	for _, brand := range knownBrands {
		for _, alias := range brand.Aliases {
			if strings.Contains(haystack, normalizeText(alias)) {
				return brand
			}
		}
	}
	return brandDefinition{ID: "other", Name: "其他", Rank: 100}
}

func brandRank(id string) int {
	for _, brand := range knownBrands {
		if brand.ID == id {
			return brand.Rank
		}
	}
	return 100
}

func normalizeText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}

func normalizeRegionName(value string) string {
	value = normalizeText(value)
	for _, suffix := range []string{"壮族自治区", "回族自治区", "维吾尔自治区", "特别行政区", "自治区", "省", "市"} {
		value = strings.TrimSuffix(value, suffix)
	}
	return value
}
