# cfip-lite-mini

一个极简、高并发、低资源占用的 CIDR/IP HTTPS 可用性扫描器。

从用户指定的 IP 地址段中快速筛选出能够通过 **指定域名 SNI + HTTP Host** 正常访问目标网站的 IP，并按实际 HTTPS 响应耗时排序。

```
用户提供 CIDR/IP/Range
        ↓
展开 IP（迭代器，不整段载入内存）
        ↓
并发 HTTPS 请求（worker pool）
        ↓
TLS SNI = domain + HTTP Host = domain
        ↓
300ms 超时淘汰 / 非法状态码淘汰
        ↓
按响应耗时排序
        ↓
输出 best_ip.txt + result.json
```

## 特性

- 极简：核心逻辑一个二进制，零运行时依赖（无 bash/curl/openssl/python/systemd）
- 高并发：goroutine + worker pool + channel，默认 500 并发
- 低资源：IP 逐条生成，支持 IPv6，单文件约 8MB
- 可移植：Linux amd64 / arm64 / armv7、Windows amd64，适合 OpenWrt
- 标准库优先：仅一个 YAML 解析依赖 `gopkg.in/yaml.v3`

## 工作原理

对每个 IP 直接发起 HTTPS GET `/` 请求：

| 项 | 值 |
| --- | --- |
| 连接目标 | `IP:port`（强制，不经过 DNS） |
| TLS SNI | `domain`（`TLSClientConfig.ServerName`） |
| HTTP Host | `domain`（`req.Host`） |

程序不使用 `http.Get("https://" + ip)` 这类隐式实现，而是显式控制 `TLSClientConfig.ServerName`、`req.Host`、`req.URL.Host` 与 `DialContext`，确保连接目标是 IP 而握手与 Host 是目标域名。

### HTTPS / SNI / Host 原理

- **SNI（Server Name Indication）**：TLS 握手中的域名扩展。同一台服务器上托管多个域名时，服务器靠 SNI 决定返回哪张证书、交给哪个虚拟主机处理。扫描器把 SNI 固定为目标域名，因此能命中目标站点的证书与 vhost。
- **HTTP Host 头**：HTTP/1.1 必须携带 Host，反向代理和负载均衡依赖它路由请求。
- **连接目标与 SNI/Host 分离**：直接连 `IP:port`，但握手和 Host 用域名。这等价于"把 `hosts` 文件中的域名指向该 IP 后访问"，这正是本工具筛选可用 IP 的核心。
- **安全**：完整 TLS 证书校验，仅接受受信任的证书链，`ServerName` 与证书匹配。

## 安装

### 源码构建

要求 Go 1.22+。

```bash
go build -trimpath -ldflags="-s -w" -o cfip-lite-mini .
```

### Release 二进制

从 [Releases](https://github.com/nongsp/cfip-lite-mini/releases) 下载对应平台文件：

- `cfip-lite-mini-linux-amd64`
- `cfip-lite-mini-linux-arm64`
- `cfip-lite-mini-linux-armv7`
- `cfip-lite-mini-windows-amd64.exe`

下载后赋予执行权限即可：

```bash
chmod +x cfip-lite-mini-linux-amd64
```

## 快速开始

```bash
./cfip-lite-mini -cidr 43.198.0.0/16
```

或使用配置文件：

```bash
cp config.yaml config.local.yaml
# 编辑 config.local.yaml 填写你的 domain 与 ip_range
./cfip-lite-mini -config config.local.yaml
```

## CLI 参数

| 参数 | 说明 | 可重复 |
| --- | --- | --- |
| `-domain` | 目标域名，同时用于 TLS SNI 与 HTTP Host（默认 `ipv4.svi.cc.cd`） | 否 |
| `-cidr` | CIDR 段，如 `43.198.0.0/16` | 是 |
| `-ip` | 单个 IP，如 `43.198.5.166` | 是 |
| `-range` | IP 起止范围，如 `159.60.146.10-159.60.146.200` | 是 |
| `-port` | HTTPS 端口，默认 `443` | 否 |
| `-timeout` | 单 IP 总超时，默认 `300ms` | 否 |
| `-concurrency` | 并发 worker 数，默认 `500` | 否 |
| `-top` | 输出最佳 IP 数量，默认 `30` | 否 |
| `-max-ips` | 最大扫描 IP 数，默认 `1000000`，硬上限 `10000000` | 否 |
| `-user-agent` | 请求 User-Agent | 否 |
| `-config` | YAML 配置文件路径，默认 `config.yaml` | 否 |
| `-output-dir` | 输出目录，默认当前目录 | 否 |
| `-http` | 启用 yx-tools 风格的 HTTPing 测速方法（默认 `false`） | 否 |
| `-ping-times` | 启用 `-http` 后每个 IP 的测速请求次数，延迟取平均值（默认 `1`） | 否 |
| `-version` | 打印版本并退出 | 否 |
| `-h, -help` | 显示完整使用说明并退出 | 否 |

配置优先级：**CLI > config.yaml > 默认值**。只要在 CLI 中给出任一 `-cidr/-ip/-range`，`ip_range` 整体被 CLI 覆盖。

## yx-tools HTTPing 扫描方法（-http）

借鉴 [yx-tools](https://github.com/byJoey/yx-tools) 的 `-http` 测速方法，解决大范围扫描时的连接资源问题：

- **每个 IP 独立 Transport**：探测完立即 `CloseIdleConnections()` 回收，连接不留在空闲池里等超时，避免本地临时端口被几千个候选 IP 占满。
- **`SetLinger(0)` RST 关闭**：测速连接用完即弃，直接发 RST 而不是四次挥手，防止连接进入 60 秒 TIME_WAIT 占住端口，波及机器上其它业务。
- **阻止重定向**：`CheckRedirect` 返回 `http.ErrUseLastResponse`，301/302 原样上报，连接不会被重新拨号到 Location 主机——这是对 SNI/Host 强制的重要补充（默认模式同样阻止重定向）。
- **多次测速取平均**：每个 IP 连续请求 `-ping-times` 次（最后一次强制 `Connection: close`），延迟取平均值，比单次请求更能反映真实链路质量。
- **不完整下载**：请求 `GET /`，只读取至多 1KB 响应体即关闭连接。

```bash
# 对 43.198.0.0/16 使用 HTTPing 方法，每 IP 测 4 次取平均延迟
./cfip-lite-mini -domain ipv4.svi.cc.cd -cidr 43.198.0.0/16 -http -ping-times 4

# 效果等同的配置文件方式
./cfip-lite-mini -config config.yaml
```

HTTPing 方法仍保持本项目的核心约束：连接目标是 `IP:port`，TLS SNI 与 HTTP Host 均为 `domain`。

## config.yaml

```yaml
domain: ipv4.svi.cc.cd

ip_range:
  - 43.198.0.0/16
  - 159.60.146.10-159.60.146.200
  - 43.198.5.166

port: 443
timeout: 300ms
concurrency: 500
top: 30
max_ips: 1000000
user_agent: cfip-lite-mini/1.0
http: false
ping_times: 1
```

字段均可省略，省略项使用默认值；`domain` 默认 `ipv4.svi.cc.cd`。

## CIDR / IP / Range 示例

三种写法可以混用：

```bash
# CIDR
./cfip-lite-mini -domain d.example.com -cidr 43.198.0.0/16

# 单个 IP
./cfip-lite-mini -domain d.example.com -ip 43.198.5.166

# IP 起止范围（闭区间）
./cfip-lite-mini -domain d.example.com -range 159.60.146.10-159.60.146.200

# 混合多条，同一参数可重复
./cfip-lite-mini -domain d.example.com \
  -cidr 43.198.0.0/16 \
  -cidr 43.199.0.0/16 \
  -ip 43.198.5.166 \
  -range 159.60.146.10-159.60.146.200

# 调整并发与超时
./cfip-lite-mini -domain d.example.com -cidr 43.198.0.0/16 \
  -concurrency 800 -timeout 500ms -top 50

# 查看版本
./cfip-lite-mini -version
```

## 平台使用

### Linux

```bash
chmod +x cfip-lite-mini-linux-amd64
./cfip-lite-mini-linux-amd64 -config config.yaml
```

### Windows

在 PowerShell 中：

```powershell
.\cfip-lite-mini-windows-amd64.exe -config config.yaml
```

### OpenWrt

适用于 arm64 路由器（如 AX 系列）或 armv7（如多数老款 MT7621 设备）：

```bash
# 上传二进制后
chmod +x /root/cfip-lite-mini
/roo./cfip-lite-mini -cidr 43.198.0.0/16 -top 10
```

`cfip-lite-mini` 是静态编译的纯 Go 程序，不需要安装任何依赖，二进制约 8MB，适合在低内存设备上运行。注意：OpenWrt 一般建议 `-concurrency` 适当调低（如 100-300）。

## Docker

```bash
# 构建镜像
docker build -t cfip-lite-mini .

# 运行：挂载配置与输出目录
docker run --rm -it \
  -v "$(pwd)/config.yaml:/data/config.yaml" \
  -v "$(pwd)/out:/data/out" \
  cfip-lite-mini -config /data/config.yaml -output-dir /data/out
```

镜像基于 `alpine`，仅含 CA 证书与静态二进制，以非 root 用户运行。

## GitHub Actions

仓库内置 `.github/workflows/release.yml`：

- 任何 push/PR：checkout → setup Go → `gofmt` 检查 → `go vet` → `go test` → `go build`
- CI 不扫描任何真实公网 CIDR
- 打 Tag（如 `v1.0.0`）时自动触发 Release，构建四个平台二进制并生成 `checksums.txt`

## Release 编译产物

| 平台 | 文件 |
| --- | --- |
| Linux amd64 | `cfip-lite-mini-linux-amd64` |
| Linux arm64 | `cfip-lite-mini-linux-arm64` |
| Linux armv7 | `cfip-lite-mini-linux-armv7` |
| Windows amd64 | `cfip-lite-mini-windows-amd64.exe` |

所有二进制使用 `CGO_ENABLED=0`、`-ldflags="-s -w"` 静态编译，版本号通过 `-X main.version=<tag>` 注入，并附带 SHA256 `checksums.txt`。

## 输出格式

控制台实时显示进度 `Progress: x/y`，结束后打印 Top 结果。

### best_ip.txt

每行一个 IP，按延迟升序：

```text
43.198.5.166
43.198.5.155
43.198.5.201
```

### result.json

```json
[
  {
    "ip": "43.198.5.166",
    "status": 200,
    "delay": "38ms",
    "score": 100
  }
]
```

排序：`delay 升序 → status 升序 → IP 升序`。评分简单固定：200=100，403=90，301/302=70。

有效状态码仅 `200 / 301 / 302 / 403`，其余一律淘汰。每次请求只读取至多 1KB 响应体后立即关闭连接。

## 性能调优

- **并发**：`-concurrency` 决定同时进行的请求数。带宽充足时可调高（500-1000）；OpenWrt、共享网络请调低（100-300）。
- **超时**：`-timeout` 越短，淘汰越快但误判越多（如跨境链路较慢）。推荐 200ms-500ms。
- **IP 总量**：`-max-ips` 防止错误配置导致资源耗尽。默认 1,000,000，硬上限 10,000,000。
- **资源**：IP 由迭代器逐条生成，不会一次性载入内存；扫描结束所有 goroutine、channel、空闲连接自动清理。

## 合法合规提醒

- 请仅扫描**你拥有**或**已获明确授权**的 IP 地址段。
- 对未授权目标进行扫描属于违规行为，可能违反当地法律或平台条款。
- 本工具仅供网络运维、服务自检与学术研究使用，请合理控制扫描速率，避免对目标造成影响。
- 使用者需自行承担因滥用导致的全部责任。

## 开发

```bash
gofmt -w .
go test ./...
go vet ./...
go build ./...
```

交叉编译验证：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build .
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build .
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build .
```

## License

[MIT](LICENSE)
