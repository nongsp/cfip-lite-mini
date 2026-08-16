# cfip

一个极简、高并发、低资源占用的 CIDR/IP HTTPS 可用性扫描器。

从用户指定的 IP 地址段中快速筛选出能够通过 **指定域名 SNI + HTTP Host** 正常访问目标网站的 IP，并按实际 HTTPS 响应耗时排序。

```
用户提供 CIDR/IP/Range
        ↓
展开 IP（迭代器，不整段载入内存）
        ↓
并发 HTTPS 请求（worker pool，默认 yx-tools 风格 HTTPing）
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
- 可移植：Linux amd64 / arm64 / armv7、Windows amd64、Android arm64，适合 OpenWrt 与 Android 设备
- 标准库优先：仅一个 YAML 解析依赖 `gopkg.in/yaml.v3`
- 双模式：主扫描（CIDR/IP/Range 全段筛选）+ `proxy` 子命令（优选反代，移植自 yx-tools）

## 工作原理

对每个 IP 直接发起 HTTPS HEAD `/` 请求（完整 HTTP 请求，含 TLS 握手和服务端响应，默认使用 yx-tools 风格的 `-http` HTTPing 扫描方法）：

| 项 | 值 |
| --- | --- |
| 连接目标 | `IP:port`（强制，不经过 DNS） |
| TLS SNI | `domain`（`TLSClientConfig.ServerName`） |
| HTTP Host | `domain`（`req.Host`） |
| 请求方法 | HEAD（默认 `-http` 模式，不含响应体） |

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
go build -trimpath -ldflags="-s -w" -o cfip .
```

### Release 二进制

从 [Releases](https://github.com/nongsp/cfip/releases) 下载对应平台文件：

- `cfip-linux-amd64`
- `cfip-linux-arm64`
- `cfip-linux-armv7`
- `cfip-windows-amd64.exe`
- `cfip-android-arm64`

下载后赋予执行权限即可：

```bash
chmod +x cfip-linux-amd64
```

## 快速开始

```bash
./cfip -cidr 43.198.0.0/16
```

或使用配置文件：

```bash
cp config.yaml config.local.yaml
# 编辑 config.local.yaml 填写你的 domain 与 ip_range
./cfip -config config.local.yaml
```

优选反代（从别人分享的结果里筛可用 `IP:port`）：

```bash
./cfip proxy -i result.csv -test -colo HKG,SIN
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
| `-http` | 启用 yx-tools 风格的 HTTPing 测速方法（默认 `true`，即默认使用） | 否 |
| `-ping-times` | 启用 `-http` 后每个 IP 的测速请求次数，延迟取平均值（默认 `1`） | 否 |
| `-version` | 打印版本并退出 | 否 |
| `-h, -help` | 显示完整使用说明并退出 | 否 |

配置优先级：**CLI > config.yaml > 默认值**。只要在 CLI 中给出任一 `-cidr/-ip/-range`，`ip_range` 整体被 CLI 覆盖。

## yx-tools HTTPing 扫描方法（-http，默认开启）

借鉴 [yx-tools](https://github.com/byJoey/yx-tools) 的 `-http` 测速方法，**默认开启**，解决大范围扫描时的连接资源问题（尤其在 Windows 上，系统临时端口范围小，非 HTTPing 的共享连接池会导致 TIME_WAIT 端口耗尽、扫不到 IP）：

- **每个 IP 独立 Transport**：探测完立即 `CloseIdleConnections()` 回收，连接不留在空闲池里等超时，避免本地临时端口被几千个候选 IP 占满。
- **`SetLinger(0)` RST 关闭**：测速连接用完即弃，直接发 RST 而不是四次挥手，防止连接进入 60 秒 TIME_WAIT 占住端口，波及机器上其它业务。
- **阻止重定向**：`CheckRedirect` 返回 `http.ErrUseLastResponse`，301/302 原样上报，连接不会被重新拨号到 Location 主机——这是对 SNI/Host 强制的重要补充（默认模式同样阻止重定向）。
- **多次测速取平均**：每个 IP 连续请求 `-ping-times` 次（最后一次强制 `Connection: close`），延迟取平均值，比单次请求更能反映真实链路质量。
- **完整 HTTP 请求**：HEAD 请求含 TLS 握手与服务端响应，不下载响应体，探测更轻量。

```bash
# 默认即 HTTPing（无需 -http），每 IP 测 4 次取平均延迟
./cfip -cidr 43.198.0.0/16 -ping-times 4

# 显式指定（默认已开启）
./cfip -cidr 43.198.0.0/16 -http

# 需要关闭时
./cfip -cidr 43.198.0.0/16 -http=false

# 效果等同的配置文件方式
./cfip -config config.yaml
```

HTTPing 方法仍保持本项目的核心约束：连接目标是 `IP:port`，TLS SNI 与 HTTP Host 均为 `domain`。

## proxy 子命令（优选反代）

完整移植 [yx-tools](https://github.com/byJoey/yx-tools) 的 `yx proxy` 优选反代流程：把别人分享的测速结果 CSV、资产导出的 `ip,port` 表或每行 `IP:端口` 的裸列表，提取成反代列表后，用与主扫描完全相同的检测方法重新测速，按延迟排序输出可用的 `IP:port`。

```bash
# 只提取列表（不测速）
./cfip proxy -i result.csv -o ips_ports.txt

# 提取后立即测速
./cfip proxy -i result.csv -test

# 只保留回源地区为香港/新加坡的（自动启用 HTTPing 以读取响应头地区码）
./cfip proxy -i list.txt -test -colo HKG,SIN
```

### 检测方法（与 yx-tools proxy 完全一致）

- **来源解析**：优先按 CSV 解析，兼容 yx-tools 中文表头 `IP 地址`/`端口` 与资产导出的英文小写表头 `ip`/`port`；CSV 读不出时退回逐行解析裸列表，每行 `IP[:端口]`，支持 `#` 备注（GitHub 风格 `IP:端口#备注`）与纯 IPv6。
- **每 IP 独立端口（PortMapping）**：列表里每个候选携带自己的端口（默认 443），测速时按各自的端口连接——这正是反代测速与全段扫描的本质区别，对应 yx-tools 的 `PortMapping`。
- **HTTPing 检测**：与主扫描的 `-http` 方法相同（每 IP 独立 Transport、`SetLinger(0)` RST 关闭、阻止重定向、`-ping-times` 次取平均延迟），完整走 TLS 握手与服务端响应。
- **延迟上限（-tl）**：平均延迟超过该值（ms）的候选直接淘汰，默认 0 不限制。
- **地区过滤（-colo）**：从 CDN 响应头提取地区码（Cloudflare `cf-ray`、AWS CloudFront `x-amz-cf-pop`、Fastly `x-served-by`、BunnyCDN、CDN77、Gcore），只保留匹配指定地区的候选。地区码只能从 HTTP 响应头获取，因此指定 `-colo` 时自动启用 HTTPing。
- **输出**：`best_ip.txt` 每行一个 `IP:port`（按延迟升序），`result.json` 带 `port`、`colo` 字段。

### proxy 参数

| 参数 | 说明 |
| --- | --- |
| `-i` | 来源文件：测速结果 CSV 或每行 `IP[:端口]` 的列表（默认 `result.csv`） |
| `-o` | 输出的反代列表文件（默认 `ips_ports.txt`） |
| `-take` | 从来源取前 N 条，0 表示全部 |
| `-test` | 生成列表后直接对该列表测速 |
| `-domain` | 目标域名，用于 TLS SNI 与 HTTP Host（默认 `ipv4.svi.cc.cd`） |
| `-port` | 列表中未指定端口的 IP 使用的端口（默认 `443`） |
| `-timeout` | 单 IP 总超时，默认 `300ms` |
| `-t` | 并发 worker 数，默认 `500` |
| `-n` | 输出最佳 IP 数量，默认 `30` |
| `-tl` | 平均延迟上限 ms，超过的淘汰（0 不限制） |
| `-colo` | 期望地区码，逗号分隔，如 `HKG,SIN`（指定后自动启用 HTTPing） |
| `-http` | 用 HTTPing 测速（默认 `true`） |
| `-ping-times` | 启用 `-http` 时每个 IP 的测速请求次数（默认 `1`） |
| `-user-agent` | 请求 User-Agent |
| `-mt` | 整轮测速时长上限（秒），0 不限 |
| `-output-dir` | 输出目录（默认当前目录） |

```bash
# 来源是 yx-tools / 本工具导出的 CSV
./cfip proxy -i result.csv -test -take 50

# 来源是别人分享的每行 IP:端口 列表，带备注
./cfip proxy -i shared_list.txt -test -colo HKG,SIN -tl 300

# 列表中端口不是 443（如 2053/8443），每个 IP 用自己的端口连接
./cfip proxy -i custom_ports.txt -test

# 查看 proxy 帮助
./cfip proxy -h
```

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
user_agent: cfip/1.0
http: true
ping_times: 1
```

字段均可省略，省略项使用默认值；`domain` 默认 `ipv4.svi.cc.cd`。

## CIDR / IP / Range 示例

三种写法可以混用：

```bash
# CIDR
./cfip -domain d.example.com -cidr 43.198.0.0/16

# 单个 IP
./cfip -domain d.example.com -ip 43.198.5.166

# IP 起止范围（闭区间）
./cfip -domain d.example.com -range 159.60.146.10-159.60.146.200

# 混合多条，同一参数可重复
./cfip -domain d.example.com \
  -cidr 43.198.0.0/16 \
  -cidr 43.199.0.0/16 \
  -ip 43.198.5.166 \
  -range 159.60.146.10-159.60.146.200

# 调整并发与超时
./cfip -domain d.example.com -cidr 43.198.0.0/16 \
  -concurrency 800 -timeout 500ms -top 50

# 查看版本
./cfip -version
```

## 平台使用

### Linux

```bash
chmod +x cfip-linux-amd64
./cfip-linux-amd64 -config config.yaml
```

### Windows

在 PowerShell 中：

```powershell
.\cfip-windows-amd64.exe -config config.yaml
```

### OpenWrt

适用于 arm64 路由器（如 AX 系列）或 armv7（如多数老款 MT7621 设备）：

```bash
# 上传二进制后
chmod +x /root/cfip
/root/cfip -cidr 43.198.0.0/16 -top 10
```

`cfip` 是静态编译的纯 Go 程序，不需要安装任何依赖，二进制约 8MB，适合在低内存设备上运行。注意：OpenWrt 一般建议 `-concurrency` 适当调低（如 100-300）。

### Android

`cfip-android-arm64` 是面向 Android aarch64（armv8）内核的静态 ELF 二进制，不依赖 glibc（Android 上不存在 glibc），由 Android 自带的 bionic libc 之外完全自包含，`adb push` 到设备后即可直接运行：

```bash
# 上传到设备 /data/local/tmp（应用沙箱外，可执行）
adb push cfip-android-arm64 /data/local/tmp/cfip
adb shell chmod +x /data/local/tmp/cfip
adb shell /data/local/tmp/cfip -cidr 43.198.0.0/16 -top 10
```

如需在 Termux 等常规 Linux 环境运行 Android 版，`cfip-linux-arm64` 是更合适的选择；Android 版专为 Android 内核适配。

## Docker

```bash
# 构建镜像
docker build -t cfip .

# 运行：挂载配置与输出目录
docker run --rm -it \
  -v "$(pwd)/config.yaml:/data/config.yaml" \
  -v "$(pwd)/out:/data/out" \
  cfip -config /data/config.yaml -output-dir /data/out
```

镜像基于 `alpine`，仅含 CA 证书与静态二进制，以非 root 用户运行。

## GitHub Actions

仓库内置 `.github/workflows/release.yml`：

- 任何 push/PR：checkout → setup Go → `gofmt` 检查 → `go vet` → `go test` → `go build`
- CI 不扫描任何真实公网 CIDR
- 打 Tag（如 `v1.0.0`）时自动触发 Release，构建五个平台二进制并生成 `checksums.txt`

## Release 编译产物

| 平台 | 文件 |
| --- | --- |
| Linux amd64 | `cfip-linux-amd64` |
| Linux arm64 | `cfip-linux-arm64` |
| Linux armv7 | `cfip-linux-armv7` |
| Windows amd64 | `cfip-windows-amd64.exe` |
| Android arm64 | `cfip-android-arm64` |

所有二进制使用 `CGO_ENABLED=0`、`-ldflags="-s -w"` 静态编译，版本号通过 `-X main.version=<tag>` 注入，并附带 SHA256 `checksums.txt`。`cfip-android-arm64` 为面向 Android aarch64 内核的静态 ELF（`GOOS=android`），不依赖 glibc。

## 输出格式

控制台实时显示进度 `Progress: x/y`，结束后打印 Top 结果。

### best_ip.txt

每行一个 IP，按延迟升序：

```text
43.198.5.166
43.198.5.155
43.198.5.201
```

`proxy` 模式下每行是 `IP:port`：

```text
43.198.100.202:443
43.198.104.5:443
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

`proxy` 模式下额外包含 `port` 与 `colo`（地区码）字段：

```json
[
  {
    "ip": "43.198.100.202",
    "port": 443,
    "status": 403,
    "delay": "177ms",
    "score": 90,
    "colo": "HKG"
  }
]
```

排序：`delay 升序 → status 升序 → IP 升序`。评分简单固定：200=100，403=90，301/302=70。

有效状态码仅 `200 / 301 / 302 / 403`，其余一律淘汰。默认使用 HEAD 请求（`-http` HTTPing 模式），不下载响应体；仅当 `-http=false` 时使用 GET 请求并最多读取 1KB 响应体。

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
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build .
CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build .
CGO_ENABLED=0 GOOS=linux   GOARCH=arm   GOARM=7 go build .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build .
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build .
```

## License

[MIT](LICENSE)
