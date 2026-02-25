# Apple CDN Network Bench

> Apple CDN (`mensura.cdn-apple.com`) 的下载/上传/延迟测速工具。

---

## Go 版本（推荐）

使用 Go 重写，零外部依赖（无需 curl / awk / dd / pv 等），单一二进制即可运行。

### 环境要求

- Go 1.22+（仅构建时需要）

### 构建 & 运行

```bash
# 直接运行
go run ./cmd/speedtest/

# 构建二进制
go build -o speedtest ./cmd/speedtest/
./speedtest

# 查看版本
./speedtest --version

# 查看帮助
./speedtest --help

# 多平台交叉编译（输出到 dist/）
bash scripts/build.sh
```

### 一键安装（Linux/macOS）

```bash
curl -fsSL https://raw.githubusercontent.com/tsosunchia/apple-cdn-network-bench/main/scripts/install.sh | bash
```

- 自动识别系统与架构（`linux`/`darwin` + `amd64`/`arm64`）
- 从 GitHub Releases 下载最新版本并校验 `sha256`
- 默认安装到用户目录（优先复用 PATH 中可写目录，否则使用 `~/.local/bin`，无需 sudo）
- 可通过 `INSTALL_DIR` 指定安装目录，例如：`INSTALL_DIR="$HOME/bin" bash scripts/install.sh`

### 本地质量检查

```bash
bash scripts/check.sh
```

包含：`gofmt -l` 格式检查、`go vet`、`go test`、`go test -race`。

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DL_URL` | `https://mensura.cdn-apple.com/api/v1/gm/config` 下的 large URL | 下载测试地址 |
| `UL_URL` | `https://mensura.cdn-apple.com/api/v1/gm/config` 下的 slurp URL | 上传测试地址 |
| `LATENCY_URL` | `https://mensura.cdn-apple.com/api/v1/gm/config` 下的 small URL | 延迟测试地址 |
| `MAX` | `2G` | 每线程最大传输量（支持 K/M/G/T 以及 KiB/MiB/GiB/TiB） |
| `TIMEOUT` | `10` | 每线程传输超时（秒） |
| `THREADS` | `4` | 多线程并发数 |
| `LATENCY_COUNT` | `20` | 空载延迟采样次数 |

示例：

```bash
TIMEOUT=5 MAX=1G THREADS=8 LATENCY_COUNT=10 go run ./cmd/speedtest/
```

### 命令行参数（优先级高于环境变量）

| 参数 | 对应环境变量 | 说明 |
|------|--------------|------|
| `--dl-url` | `DL_URL` | 下载测试地址 |
| `--ul-url` | `UL_URL` | 上传测试地址 |
| `--latency-url` | `LATENCY_URL` | 延迟测试地址 |
| `--max` | `MAX` | 每线程最大传输量 |
| `--timeout` | `TIMEOUT` | 每线程传输超时（秒） |
| `--threads` | `THREADS` | 多线程并发数 |
| `--latency-count` | `LATENCY_COUNT` | 空载延迟采样次数 |

示例：

```bash
./speedtest --timeout 5 --max 1G --threads 8 --latency-count 10
```

### 输出模式

- **TTY**（终端直连）：彩色输出 + 实时进度刷新（`\r` 覆盖刷新）
- **非 TTY**（管道 / CI）：纯文本输出，无 ANSI 转义，无进度行

### 项目结构

```
cmd/speedtest/main.go       入口，信号处理
internal/
  config/    配置加载 & 校验 & 单位解析
  netx/      HTTP/2 客户端工厂 + 端点固定（--resolve 等效）
  endpoint/  DoH 解析 + ip-api 地理信息 + 节点选择
  latency/   空载/负载延迟采样 & 统计
  transfer/  下载/上传传输（单/多线程、双限制）
  runner/    测试流程编排
  render/    事件总线 + TTY/Plain 渲染器
```

### 运行测试

```bash
go test ./... -count=1        # 全部测试
go test -race ./... -count=1  # 含竞态检测
bash scripts/check.sh         # 本地完整检查（格式 + vet + test + race）
```

### 退出码

| 码 | 含义 |
|----|------|
| 0 | 全部成功 |
| 1 | 配置错误（参数非法） |
| 2 | 完成但部分查询降级（如 ip-api 不可达） |
| 130 | 被信号中断（Ctrl+C） |

### CI / CD

- **CI**（[.github/workflows/ci.yml](.github/workflows/ci.yml)）：push / PR 触发，Go 稳定版 + 上一稳定版矩阵，macOS + Linux，缓存 go mod，运行 `check.sh`。
- **Release**（[.github/workflows/release.yml](.github/workflows/release.yml)）：`v*` tag 触发，先跑测试，再用 `build.sh` 产出 5 平台二进制（含 `windows/amd64`）+ sha256 校验文件，上传到 GitHub Release。

### 节点选择逻辑

1. 通过 AliDNS DoH 查询 `mensura.cdn-apple.com` 的 A 记录。
2. 用 ip-api 查询每个 IP 的地域 / ASN 信息。
3. 交互终端下可手动选择节点；非交互环境默认选择第 1 个。
4. 选中后通过 HTTP 客户端 DialContext 固定连接目标（等效于 `curl --resolve`）。

---

## Shell 脚本版本（原始）

### 包含脚本

- `apple-cdn-speedtest.sh`：完整测速（空载延迟、单/多线程下载、单/多线程上传）
- `apple-cdn-download-test.sh`：仅下载测速
- `apple-cdn-upload-test.sh`：仅上传测速

## 环境依赖

脚本启动时会进行环境检查，并给出缺失依赖与安装提示。

必需依赖（主脚本）：

- `curl`（必须支持 HTTP/2）
- `awk`
- `grep`
- `sed`
- `sort`
- `mktemp`
- `dd`
- `wc`
- `tr`
- `head`

必需依赖（单项脚本）：

- 下载脚本：`curl`, `awk`, `grep`, `head`, `dd`, `mktemp`, `date`, `cat`
- 上传脚本：`curl`, `awk`, `dd`, `mktemp`, `date`, `cat`

可选依赖：

- `pv`：用于实时进度显示（缺失不影响测速结果）
- `getent`/`dig`/`host`/`nslookup`/`ping`：用于 DNS 辅助解析（缺失仅影响部分展示信息）

网络访问要求：

- Apple 测速端点：`https://mensura.cdn-apple.com`
- AliDNS DoH：`https://dns.alidns.com`（节点选择）
- ip-api：`http://ip-api.com`（节点地理信息）

## 快速开始

### 1) 完整测速

```bash
sh apple-cdn-speedtest.sh
```

### 2) 下载测速

```bash
sh apple-cdn-download-test.sh
```

### 3) 上传测速

```bash
sh apple-cdn-upload-test.sh
```

## 你也可以直接使用 nxtrace 提供的在线脚本

```bash
# 上传速度
curl -sL nxtrace.org/upload |bash

# 下载速度
curl -sL nxtrace.org/download |bash

# One-key
curl -sL nxtrace.org/speedtest |bash
```

也支持 `https`，将域名前加上 `https://` 即可。

也可以先下载再执行：

```bash
wget https://nxtrace.org/speedtest
chmod +x speedtest
./speedtest
```

## 常用环境变量（Shell 版）

主脚本（`apple-cdn-speedtest.sh`）：

- `DL_URL`：下载 URL
- `UL_URL`：上传 URL
- `LATENCY_URL`：延迟探测 URL
- `MAX`：每线程最大传输量（默认 `2G`）
- `TIMEOUT`：每线程测试时长（默认 `10` 秒）
- `THREADS`：多线程并发数（默认 `4`）
- `LATENCY_COUNT`：空载延迟采样次数（默认 `20`）

示例：

```bash
TIMEOUT=5 MAX=1G THREADS=8 LATENCY_COUNT=10 sh apple-cdn-speedtest.sh
```

## 节点选择逻辑（Shell 版）

1. 通过 AliDNS DoH 查询 `mensura.cdn-apple.com` 的 A 记录。
2. 用 ip-api 查询每个 IP 的地域/ASN 信息。
3. 交互终端下可手动选择节点；非交互环境默认选择第 1 个。
4. 选中后通过 `curl --resolve host:443:IP` 固定后续测试连接目标。

## 许可证

本项目采用 **GNU General Public License v3.0 (GPL-3.0-or-later)** 开源。

- 许可证全文见 `LICENSE`。
