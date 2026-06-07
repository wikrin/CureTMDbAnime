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
             # 如果需要配置上游或代理，可以在此处添加环境变量
             # - PROXY=http://host.docker.internal:7890
          restart: unless-stopped
    ```

2.  **启动服务**

    ```bash
    docker-compose up -d
    ```

## 配置说明

项目支持环境变量和命令行参数配置，命令行参数优先级高于环境变量。主要配置项如下：

| 环境变量 | 命令行参数 | 默认值 | 说明 |
|--------|------------|--------|------|
| `HOST` | `--host` | `0.0.0.0` | 服务监听地址 |
| `PORT` | `--port` | `8632` | 服务监听端口 |
| `TMDB_API_URL` | `--tmdb-api-url` | `https://api.themoviedb.org` | TMDB API 上游地址 |
| `CURE_SOURCE` | `--cure-source` | GitHub Raw URL | 修正规则的源数据 URL (JSON) |
| `PROXY` | `--proxy` | 空 | 可选：请求上游时使用的 HTTP/HTTPS 代理 |
| `DATA_DIR` | `--data-dir` | `/opt/data` | 数据存储目录 |
| `BANGUMI_API_URL` | `--bangumi-api-url` | `https://api.bgm.tv/` | Bangumi API 上游地址 |
| `BANGUMI_USE_PROXY` | `--bangumi-use-proxy` | `false` | Bangumi API 请求是否使用 `PROXY` |

示例：

```bash
BANGUMI_API_URL=https://api.bgm.tv/ BANGUMI_USE_PROXY=true ./curetmdbanime

./curetmdbanime --bangumi-api-url=https://api.bgm.tv/ --bangumi-use-proxy
```

## API 接口

服务启动后，主要拦截并处理以下路径的请求：

- `GET /3/tv/{tmdb_id}`: 获取剧集详情（可能返回修正后的数据）。
- `GET /3/tv/{tmdb_id}/season/{season_number}`: 获取季度详情（支持虚拟季度）。
- `GET /3/tv/{tmdb_id}/season/{season_number}/episode/{episode_number}`: 获取单集详情。

其他未匹配 `/3/tv/*` 的请求将被直接代理到 TMDB 上游。
