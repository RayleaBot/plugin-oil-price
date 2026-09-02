package plugin

import (
	"encoding/json"
	"strconv"
	"strings"
)

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func intValue(value any) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(stringValue(value)))
	if err != nil {
		return 0
	}
	return parsed
}

func boolValue(value any) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	parsed, _ := strconv.ParseBool(strings.TrimSpace(stringValue(value)))
	return parsed
}
