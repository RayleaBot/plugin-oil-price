# 油价查询

RayleaBot 独立插件 · `raylea.oil-price`

查询各省当前参考油价，并按省、市或区县展示中国石油、中国石化、中国海油、壳牌、延长石油等加油站目录。

## 数据边界

- 地区参考价来自聚合数据“全国省市今日油价”，显示 92、95、98 号汽油和 0 号柴油；上游未提供的油号显示“暂无”。
- 地区解析和加油站目录来自高德 Web 服务 API。
- 高德站点目录不提供加油站挂牌价。插件不会把省级参考价伪装成某座加油站的实际价格。
- 会员价、活动价和现场挂牌价可能不同，最终以加油站现场公示为准。

## 配置

安装后在插件设置中配置以下非敏感项：

| 配置项 | 默认值 | 说明 |
| --- | --- | --- |
| `default_area` | `北京` | 命令未指定地区时使用 |
| `station_limit` | `5` | 每次最多展示 1–10 个站点 |
| `price_cache_minutes` | `60` | 地区参考价缓存 5–1440 分钟 |
| `station_cache_minutes` | `720` | 站点目录缓存 10–10080 分钟 |

还需要在插件 secrets 中配置：

| Secret | 用途 |
| --- | --- |
| `juhe_api_key` | 聚合数据“全国省市今日油价”API Key |
| `amap_api_key` | 高德 Web 服务 API Key，用于城市解析和站点目录 |

密钥不会写入配置、缓存或日志。聚合数据密钥通过 POST 正文发送；高德要求 Key 位于查询参数，因此插件只请求固定的 `restapi.amap.com` HTTPS 地址，并阻止跨主机重定向。

## 使用

命令前缀以 RayleaBot 管理面配置为准，下面按默认前缀 `/` 书写：

```text
/油价
/油价 广东
/油价 深圳 95
/油价 深圳 中石化
/油价 深圳南山 0号柴油 中石油
/油站 深圳南山
/油站 成都 中石化
```

品牌别名支持：

- `中国石油`、`中石油`、`PetroChina`
- `中国石化`、`中石化`、`Sinopec`
- `中国海油`、`中海油`、`CNOOC`
- `壳牌`、`Shell`
- `延长石油`、`延长`

## 失败与降级

- 油价或站点上游短暂失败时，插件返回最近一次成功缓存并明确标记缓存时间。
- 没有 `amap_api_key` 时，省级查询仍可返回参考油价，但不显示城市站点。
- 城市查询需要高德解析其省级归属；无法确认归属时不会猜测油价。
- 缓存中不保存任何 API Key。

## 开发

本插件是独立 Go module，不随 RayleaBot 主仓库发布。

```powershell
go test -race ./...
golangci-lint run ./...
go run github.com/RayleaBot/RayleaBot/sdk/go/cmd/raylea-plugin build-go --plugin . --backend ./cmd/oil-price --target windows-x64 --out dist
```

本地联调通过 RayleaBot 的 `plugin-workspace.local.json` 连接当前插件仓库；不要向插件提交本机 `replace`、`go.work` 或 SDK 镜像。

## License

[MIT](./LICENSE)
