# 油价查询

RayleaBot 独立插件 · `raylea.oil-price`

免 API Key 查询各省、市、区县对应价区的最新公开油价，并展示城市中心附近的中国石油、中国石化、中国海油、壳牌、延长石油等加油站。

## 数据来源与边界

- 油价来自 [chinese-oil-price-data](https://github.com/luckkyboy/chinese-oil-price-data)（MIT）。该项目汇集国家发展改革委和各省级发展改革委公开调价公告，插件优先读取 GitHub Raw，失败时切换 jsDelivr 镜像。
- 地区映射覆盖省、市、区县和存在分价区的地区。展示值是最近一次公开调价后的政府指导价或最高零售价，不是分钟级行情，也不是某座加油站的实时挂牌价。
- 上游没有发布的油号会显示“暂无”。当前公开数据通常包含 89、92、95 号汽油和 0 号柴油，部分价区不提供 98 号汽油。
- 加油站目录来自 [OpenStreetMap](https://www.openstreetmap.org/copyright)（ODbL），通过 Nominatim 定位地区中心，再通过 Overpass 查询附近站点。社区数据可能缺失、过期或跨越相邻行政区。
- 会员价、活动价和现场挂牌价可能不同，最终以加油站现场公示为准。

插件不抓取来源不明的网页接口，不内置他人的公共 Token，也不要求用户申请或配置 Key。

## 配置

所有配置均为非敏感项：

| 配置项 | 默认值 | 说明 |
| --- | --- | --- |
| `default_area` | `北京` | 命令未指定地区时使用 |
| `station_limit` | `5` | 每次最多展示 1–10 个站点 |
| `station_radius_km` | `15` | 以地区中心为圆心查询 5–30 公里 |
| `price_cache_minutes` | `60` | 油价与地区映射缓存 5–1440 分钟 |
| `station_cache_minutes` | `720` | 站点目录缓存 10–10080 分钟 |

## 使用

命令前缀以 RayleaBot 管理面配置为准，下面按默认前缀 `/` 书写：

```text
/油价
/油价 广东
/油价 深圳 95
/油价 深圳 中石化
/油价 云南临沧 0号柴油
/油站 深圳南山
/油站 成都 中石化
/油品牌
```

`/油品牌` 展示可查询的油号、支持筛选的加油站品牌及常用别名，不访问网络。

品牌别名支持：

- `中国石油`、`中石油`、`PetroChina`
- `中国石化`、`中石化`、`Sinopec`
- `中国海油`、`中海油`、`CNOOC`
- `壳牌`、`Shell`
- `延长石油`、`延长`

## 失败与降级

- GitHub Raw 不可用时自动切换 jsDelivr；两者均失败时返回最近一次成功缓存，并明确标记缓存时间。
- Nominatim 请求遵守公共服务每秒最多一次的限制，地区解析缓存 7 天。
- 两个 Overpass 公共实例按顺序降级；站点请求失败时返回最近一次成功缓存。
- 油价与站点目录相互独立：站点服务失败不会阻止油价查询，`/油站` 也不依赖油价数据源。

## 开发

本插件是独立 Go module，不随 RayleaBot 主仓库发布。

```powershell
go test ./...
go vet ./...
golangci-lint run ./...
$env:RAYLEA_PLUGIN_BUILD_USE_WORKSPACE = "1"
go run github.com/RayleaBot/RayleaBot/sdk/go/cmd/raylea-plugin build-go --plugin . --backend ./cmd/oil-price --target windows-x64 --out dist
```

本地联调通过 RayleaBot 的 `plugin-workspace.local.json` 连接当前插件仓库；本地 `go.work` 构建需要启用 `RAYLEA_PLUGIN_BUILD_USE_WORKSPACE`。不要向插件提交本机 `replace`、`go.work` 或 SDK 镜像。

## License

[MIT](./LICENSE)
