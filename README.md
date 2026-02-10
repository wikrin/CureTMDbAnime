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
    go mod tidy
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
          image: celebsev/curetmdbanime:latest # 假设此镜像是可用的，否则需要用户自行构建
          container_name: curetmdbanime
          network_mode: bridge
          ports:
             - "8632:8632"
          volumes:
            - ./data:/opt/data # 持久化
          environment:
             - TZ=Asia/Shanghai
             # 如果需要配置上游或代理，可以在此处添加环境变量
             # - PROXY=http://host.docker.internal:7890
          restart: unless-stopped
    ```

2.  **启动服务**

    ```bash
    docker-compose up -d
    ```

## 配置说明

项目使用环境变量进行配置。主要配置项如下：

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `HOST` | `0.0.0.0` | 服务监听地址 |
| `PORT` | `8632` | 服务监听端口 |
| `TMDB_UPSTREAM_URL` | `https://api.themoviedb.org` | TMDB API 上游地址 |
| `CURE_SOURCE` | (GitHub Raw URL) | 修正规则的源数据 URL (JSON) |
| `PROXY` | `None` | 可选：请求上游时使用的 HTTP 代理 |

## API 接口

服务启动后，主要拦截并处理以下路径的请求：

- `GET /3/tv/{tmdb_id}`: 获取剧集详情（可能返回修正后的数据）。
- `GET /3/tv/{tmdb_id}/season/{season_number}`: 获取季度详情（支持虚拟季度）。
- `GET /3/tv/{tmdb_id}/season/{season_number}/episode/{episode_number}`: 获取单集详情。

其他未匹配 `/3/tv/*` 的请求将被直接代理到 TMDB 上游。

## 项目结构

```
.
├── go.mod                  # Go 模块文件，定义项目依赖和模块路径
├── go.sum                  # Go 模块的校验和文件
├── main.go                 # 程序入口点
├── Dockerfile              # Docker 容器构建文件
├── .dockerignore           # Docker 忽略文件
├── .gitignore              # Git 忽略文件
├── LICENSE                 # 许可证文件
└── internal/               # 内部包，包含项目核心逻辑
    ├── api/                # API 路由和处理函数 (proxy.go, router.go, tv_routes.go)
    ├── collection/         # 数据集合处理工具 (collection.go)
    ├── config/             # 配置管理 (config.go)
    ├── logger/             # 日志工具 (logger.go)
    ├── model/              # 数据模型和业务逻辑 (common.go, entry.go, error.go, logic.go, tmdb.go)
    ├── net/                # 网络请求客户端和相关工具 (request.go)
    ├── processor/          # 数据处理核心逻辑 (splitter.go, upstream.go)
    ├── providers/          # 外部数据源（如 Bangumi、Cure Source）集成 (bangumi.go, curetmdb.go)
    └── service/            # 业务服务层，包含具体业务逻辑实现 (tv_service.go)
