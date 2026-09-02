package plugin

import (
	"context"
	"fmt"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

// Run starts the RayleaBot oil-price plugin runtime.
func Run(ctx context.Context) error {
	return rayleabot.Run(ctx, rayleabot.Options{}, rayleabot.HandlerFunc(handleEvent))
}

func handleEvent(ctx context.Context, event *rayleabot.EventContext) error {
	command := event.Event.Command()
	if command != "油价" && command != "油站" {
		return event.Result(map[string]any{"handled": false})
	}
	config := settingsFromConfig(event.Config)
	input, err := parseQueryInput(command, event.Event.Args(), config.DefaultArea)
	if err != nil {
		return event.SendText("油价查询参数错误：" + err.Error())
	}
	result, err := newService(config, nil, nil).query(ctx, event.Actions(), input)
	if err != nil {
		return event.SendText(fmt.Sprintf("油价查询失败：%s", err))
	}
	return event.SendText(formatQueryResult(input, result))
}
