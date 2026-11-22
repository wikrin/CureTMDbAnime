# CureTMDbAnime (TMDB Cure Proxy)

这是一个基于 Go 和 Gin 框架构建的 TMDB (The Movie Database) 代理服务。它的主要功能是通过中间层动态重组季数和集数结构，使其更符合常见的番剧季度划分习惯（例如将一部被 TMDB 归为单季的长篇动画拆分为多个季度）。

## 功能特点

- **透明代理**：对于不需要修正的请求，作为透明代理直接转发给 TMDB 上游。
- **元数据重组**：针对特定列表中的番剧，拦截 API 请求并根据预定义逻辑重写返回的 JSON 数据。
    - 支持将原始的长篇季度拆分为多个“虚拟季度”。
    - 自动映射集数编号。
- **配置灵活**：支持自定义上游地址和修正规则源（Cure Source）。
- **高性能**：基于 Go 和 Gin 框架，使用高效的网络处理请求。
- **Docker 支持**：提供 Dockerfile，方便容器化部署。

## 快速开始

### 本地运行

1.  **环境要求**
    - Go 1.24+

2.  **安装依赖**
    ```bash
    go mod download
    ```

3.  **运行服务**
    ```bash
    # 直接运行
    go run main.go

    # 或者编译后运行
    go build -o curetmdbanime .
    ./curetmdbanime
    ```

### Docker Compose 部署

可以使用 Docker Compose 快速启动服务。

1.  **创建 `docker-compose.yml` 文件**

    ```yaml
    services:
      curetmdbanime:
          image: celebsev/curetmdbanime:latest
          container_name: curetmdbanime
          network_mode: bridge
          ports:
             - "8632:8632"
          volumes:
            - ./data:/opt/data # 持久化
          environment:
             - TZ=Asia/Shanghai
             # 可选：管理接口 Bearer 令牌
             # - MANAGEMENT_TOKEN=replace-with-a-random-secret
             # 如果需要配置上游或代理，可以在此处添加环境变量
             # - PROXY=http://host.docker.internal:7890
          restart: unless-stopped
    ```

2.  **启动服务**

    ```bash
    docker-compose up -d
    ```

## 配置说明

项目支持环境变量和命令行参数配置，优先级为：命令行参数 > 环境变量 > 默认值。空环境变量视为未设置，命令行支持显式传入空值。使用 `--help` 查看完整说明。

| 环境变量 | 命令行参数 | 默认值 | 说明 |
|--------|------------|--------|------|
| `MANAGEMENT_TOKEN` | `--management-token` | 空 | 可选管理令牌，设置后日志和上游检测接口要求 Bearer 鉴权 |
| `HOST` | `--host` | `0.0.0.0` | 服务监听地址 |
| `PORT` | `--port` | `8632` | 服务监听端口 |
| `DEBUG` | `--debug` | `false` | 启用调试日志 |
| `TMDB_API_URL` | `--tmdb-api-url` | `https://api.themoviedb.org` | TMDB API 上游地址 |
| `CURE_SOURCE` | `--cure-source` | `https://raw.githubusercontent.com/wikrin/CureTMDb/main/tv.json` | 修正规则的源数据 URL (JSON) |
| `PROXY` | `--proxy` | 空 | 可选：请求上游时使用的 HTTP/HTTPS 代理 |
| `DATA_DIR` | `--data-dir` | `/opt/data` | 数据存储目录，日志位于 `logs/` 子目录 |
| `LOG_MAX_SIZE_MIB` | `--log-max-size-mib` | `10` | 单个日志文件上限（MiB），正整数 |
| `LOG_MAX_FILES` | `--log-max-files` | `5` | 日志文件总数上限（含当前文件），正整数 |
| `BANGUMI_API_URL` | `--bangumi-api-url` | `https://api.bgm.tv/` | Bangumi API 上游地址 |
| `BANGUMI_USE_PROXY` | `--bangumi-use-proxy` | `false` | Bangumi API 请求是否使用 `PROXY` |
| `REFERENCE_PRIORITY` | `--reference-priority` | `tvdb,bangumi` | 分季参照源优先级，逗号分隔 |
| `TVDB_API_KEY` | `--tvdb-api-key` | 官方产物内置；源码构建为空 | TVDB API 密钥，可覆盖构建时内置密钥 |
| `TVDB_API_URL` | `--tvdb-api-url` | `https://api4.thetvdb.com/v4` | TVDB API 上游地址 |

示例：

```bash
BANGUMI_API_URL=https://api.bgm.tv/ BANGUMI_USE_PROXY=true ./curetmdbanime

./curetmdbanime --bangumi-api-url=https://api.bgm.tv/ --bangumi-use-proxy

# 使用 TVDB 优先
REFERENCE_PRIORITY=tvdb,bangumi TVDB_API_KEY=your-api-key ./curetmdbanime
```

分季参照源的执行顺序固定为 `CureTMDb -> REFERENCE_PRIORITY -> TMDB 原始数据`。
`CureTMDb` 不可通过配置调整；`bangumi` 和 `tvdb` 可以按需调整顺序。

源码不包含 TVDB API 密钥。官方 Release 二进制及 Docker 镜像在构建时注入密钥，运行时仍可通过 `TVDB_API_KEY` 或 `--tvdb-api-key` 覆盖。源码构建默认没有密钥，未配置时跳过 TVDB 查询，其他功能不受影响。空环境变量视为未设置；可通过 `--tvdb-api-key=` 显式禁用内置密钥。

## 构建与发布

### 官方发布

在仓库的 **Settings → Secrets and variables → Actions** 中配置以下 Secrets：

- `TVDB_API_KEY`：官方产物内置的 TVDB API 密钥，不能为空，且不应包含空白或引号。
- `DOCKERHUB_USERNAME`、`DOCKERHUB_TOKEN`：Docker Hub 发布凭据。

发布工作流会锁定一个提交，使用同一提交进行测试和编译，生成 Linux/macOS 的 amd64/arm64 二进制。密钥仅在统一编译阶段通过 Go `-ldflags -X` 注入；缺少 `TVDB_API_KEY` 时官方构建失败。Release 上传这些二进制，Docker 下载其中的 Linux 产物并按架构打包，不再编译或单独注入密钥。

Release 事件使用发布标签对应的提交；手动运行时使用 `target_commitish` 指定的提交（未填写时使用工作流触发提交），创建 Release 标签时也使用该提交。

此方案避免密钥出现在源码中，但不保证发布产物中的密钥无法被提取。

### 本地构建 Docker 镜像

`Dockerfile` 只负责打包，构建前需准备相应架构的 Linux 二进制。以下示例不内置密钥，运行时可通过 `TVDB_API_KEY` 配置：

```bash
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -o bin/curetmdbanime-linux-amd64 .

docker buildx build --platform linux/amd64 --load \
  -t curetmdbanime:local .
```

构建 arm64 镜像时，将 `GOARCH`、文件名及 `--platform` 改为对应的 `arm64`。构建多架构镜像前需准备两份 Linux 二进制；也可将对应版本的 Release 二进制下载到 `bin/` 后直接打包。

如需在自定义二进制中内置密钥，可从已有环境变量注入，不要把实际密钥写入源码或构建脚本：

```bash
# TVDB_API_KEY 已通过环境变量设置
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -ldflags "-X 'curetmdbanime/internal/config.BuiltinTVDBAPIKey=${TVDB_API_KEY}'" \
  -o bin/curetmdbanime-linux-amd64 .
```

## API 接口

服务启动后，主要拦截并处理以下路径的请求：

- `GET /3/tv/{tmdb_id}`: 获取剧集详情（可能返回修正后的数据）。
- `GET /3/tv/{tmdb_id}/season/{season_number}`: 获取季度详情（支持虚拟季度）。
- `GET /3/tv/{tmdb_id}/season/{season_number}/episode/{episode_number}`: 获取单集详情。

其他未注册的普通 API 请求直接代理到 TMDB 上游。`/management` 路径仅在本地处理，不会将管理令牌转发给上游。

### 管理接口

| 接口 | 说明 |
|------|------|
| `GET /management/health` | 公开健康检查，返回版本、运行时长、鉴权要求和日志存储状态，不检测上游 |
| `GET /management/logs` | 查询持久化日志，支持分页、级别和关键词筛选 |
| `GET /management/logs/stream` | SSE 实时日志，支持筛选和断线续传 |
| `GET /management/upstreams` | 检测 TMDB、TVDB 和 Bangumi 的可达性、HTTP 状态及耗时 |

上游检测单项超时 5 秒，不提交上游账户凭据；401/403 不代表网络故障。同一时间仅允许一次检测，重复请求返回 429。

#### 历史日志查询

| 参数 | 说明 |
|------|------|
| `limit` | 单页条数，默认 200，范围 1–2,000 |
| `after` | 查询指定 ID 之后的日志；`after=0` 从最旧保留记录开始 |
| `before` | 查询指定 ID 之前的日志，必须为正整数，与 `after` 互斥 |
| `level` | 按 `debug/info/warn/error/fatal` 筛选，不区分大小写 |
| `keyword` | 按正文子串筛选，不区分大小写，支持中文；与 `level` 同时生效 |

不带游标时返回最近一页。`items` 按 ID 升序排列，每条包含 `id/timestamp/level/message`。

- 查询更早记录：当 `has_older=true` 时，使用 `before=next_before` 继续翻页。
- 正向读取：使用 `after=next_after` 继续，`has_more` 表示是否还有更新的匹配记录。**应保存返回游标，而不是最后一条日志的 ID**，因为筛选可能跳过记录。
- `retention` 表示日志保留范围；`gap/reset` 提示历史已淘汰或游标超出范围，已删除日志无法恢复。

```bash
# 最近 200 条日志
curl -sS http://localhost:8632/management/logs

# 查询更早的日志，901 替换为上一页的 next_before
curl -sS 'http://localhost:8632/management/logs?before=901&limit=200'

# 从最旧保留记录开始，筛选包含“番剧”的错误
curl -sS --get http://localhost:8632/management/logs \
  --data-urlencode 'after=0' \
  --data-urlencode 'level=error' --data-urlencode 'keyword=番剧'
```

翻页时保持筛选条件一致。无效参数返回 400，日志存储不可用返回 503；过旧的 `before` 返回空页并提示历史已淘汰。

#### SSE 实时流

支持与历史查询相同的 `level/keyword` 筛选，默认只推送新日志。传入 `after` 或 `Last-Event-ID` 可补发保留日志后继续实时推送，`after` 优先；不支持 `before`。

```bash
# 实时查看新日志，-N 禁用 curl 输出缓冲
curl -N -sS http://localhost:8632/management/logs/stream

# 从上次收到的事件 ID 续传
curl -N -sS -H 'Last-Event-ID: 42' \
  'http://localhost:8632/management/logs/stream?level=error&keyword=tmdb'
```

事件包括 `log`（日志）、`cursor`（游标更新）、`reset`（游标失效）和 `error`（补发失败）。客户端应保存 `log` 和 `cursor` 的事件 ID，用于断线续传。

每 15 秒发送心跳，最多同时 16 个连接，超限返回 429；慢消费者会被断开。使用反向代理时需关闭缓冲，并设置足够长的读超时。

#### 日志存储

日志保存到 `DATA_DIR/logs/curetmdb.log`，按大小自动轮转，重启后仍可查询。默认每个文件最多 10 MiB，最多保留 5 个文件（含当前文件）；超限时删除旧日志，不按时间过期、不压缩。

```bash
# 调整日志保留大小和数量
DATA_DIR=./data LOG_MAX_SIZE_MIB=20 LOG_MAX_FILES=3 ./curetmdbanime

# 跟踪当前日志，轮转后继续读取
tail -F ./data/logs/curetmdb.log
```

- 仅保存应用日志，调试日志由 `DEBUG` 控制，不额外捕获 Gin、panic 或标准库的控制台输出。
- 挂载目录需可写，同一数据目录仅支持一个进程写入。缩小保留限制可能在启动时删除既有日志。
- 初始化失败会终止启动；运行中存储故障会导致历史查询或补发失败，修复后需重启。未写入或已删除的日志无法补回，突然断电可能丢失未同步记录。
- 文件、查询和实时流会脱敏常见凭据，正文最多保留 4,096 字节；**控制台输出不受此脱敏和截断保护**。

#### 可选管理鉴权

`MANAGEMENT_TOKEN`（或 `--management-token`）可选，不影响日志写入或普通代理 API：

- 未设置：管理接口无需鉴权。
- 已设置：日志查询、实时流和上游检测要求 `Authorization: Bearer <token>`，缺失或错误返回 401；健康检查仍公开。

```bash
# 令牌通过请求头传递，不放进 URL
curl -N -H "Authorization: Bearer $MANAGEMENT_TOKEN" \
  http://localhost:8632/management/logs/stream
```

日志可能包含私有信息，脱敏不能替代访问控制。公网部署建议设置独立管理令牌、使用 HTTPS 并限制网络访问。
