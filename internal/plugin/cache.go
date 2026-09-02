package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const priceCacheKey = "oilprice.bundle.public.v2"

type cacheEnvelope[T any] struct {
	StoredAt time.Time `json:"stored_at"`
	Data     T         `json:"data"`
}

type cacheActions interface {
	KVGet(context.Context, string) (rayleabot.ActionResult, error)
	KVSet(context.Context, string, any) (rayleabot.ActionResult, error)
}

func readCache[T any](ctx context.Context, actions cacheActions, key string) (cacheEnvelope[T], bool, error) {
	result, err := actions.KVGet(ctx, key)
	if err != nil {
		return cacheEnvelope[T]{}, false, fmt.Errorf("读取插件缓存: %w", err)
	}
	if !boolValue(result["exists"]) {
		return cacheEnvelope[T]{}, false, nil
	}
	raw := stringValue(result["value"])
	if raw == "" {
		return cacheEnvelope[T]{}, false, fmt.Errorf("插件缓存格式无效")
	}
	var envelope cacheEnvelope[T]
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return cacheEnvelope[T]{}, false, fmt.Errorf("解析插件缓存: %w", err)
	}
	if envelope.StoredAt.IsZero() {
		return cacheEnvelope[T]{}, false, fmt.Errorf("插件缓存缺少写入时间")
	}
	return envelope, true, nil
}

func writeCache[T any](ctx context.Context, actions cacheActions, key string, envelope cacheEnvelope[T]) error {
	raw, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("编码插件缓存: %w", err)
	}
	if _, err := actions.KVSet(ctx, key, string(raw)); err != nil {
		return fmt.Errorf("写入插件缓存: %w", err)
	}
	return nil
}

func cacheFresh(storedAt, now time.Time, ttl time.Duration) bool {
	if storedAt.IsZero() || storedAt.After(now.Add(5*time.Minute)) {
		return false
	}
	return now.Sub(storedAt) <= ttl
}

func cacheKey(prefix string, values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return fmt.Sprintf("oilprice.%s.%x", prefix, hash.Sum(nil)[:12])
}
