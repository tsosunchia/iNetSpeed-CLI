# iNetSpeed-CLI

基于 Apple CDN 的跨平台 CLI 测速工具。  
单一 Go 二进制，支持交互式测速和机器可读 JSON 输出。

![speedtest demo](./demo.svg)

## 安装

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/tsosunchia/iNetSpeed-CLI/main/scripts/install.sh | bash
```

默认安装位置：

- 普通用户优先安装到 `~/.local/bin`；如果 `~/bin` 已在 `PATH` 中，则优先复用 `~/bin`
- `root` 安装到 `/usr/local/bin`
- 若用户目录不在 `PATH`，安装脚本会自动追加到当前 shell 的 rc 文件

也可以从 Releases 下载归档包：

- [Latest Release](https://github.com/tsosunchia/iNetSpeed-CLI/releases/latest)

### Windows

PowerShell:

```powershell
irm https://raw.githubusercontent.com/tsosunchia/iNetSpeed-CLI/main/scripts/install.ps1 | iex
```

或直接下载 `speedtest-windows-amd64.zip`。

默认安装位置：

- 普通用户安装到 `%LOCALAPPDATA%\Programs\speedtest`
- 管理员安装到 `%ProgramFiles%\speedtest`
- 安装脚本会把目录写入用户级或机器级 `PATH`

## Legacy Shell 脚本

仓库仍保留以下脚本，供参考或自用：

- `scripts/apple-cdn-speedtest.sh`
- `scripts/apple-cdn-download-test.sh`
- `scripts/apple-cdn-upload-test.sh`

但它们已经退出主维护路径：

- 不再跟随 Go CLI 更新特性
- 不再纳入 CI / 发布校验
- 行为与当前 Go 版本可能逐步偏离

## 快速开始

直接运行：

```bash
speedtest
```

非交互模式：

```bash
speedtest --non-interactive
```

JSON 输出：

```bash
speedtest --json > result.json
```

固定节点：

```bash
speedtest --endpoint 17.253.85.205 --non-interactive
```

关闭元数据查询：

```bash
speedtest --no-metadata
```

## JSON 输出

`--json` 只向 `stdout` 输出单个 JSON 文档，不输出颜色、进度条或交互提示。

示例：

```json
{
  "schema_version": 1,
  "config": {
    "json": true,
    "non_interactive": true
  },
  "selected_endpoint": {
    "ip": "17.253.85.205",
    "status": "ok"
  },
  "idle_latency": {
    "status": "ok",
    "median_ms": 21.4
  },
  "rounds": [],
  "warnings": [],
  "degraded": false,
  "exit_code": 0
}
```

稳定字段：

- `config`
- `candidates`
- `selected_endpoint`
- `connection_info`
- `idle_latency`
- `rounds`
- `total_bytes`
- `warnings`
- `degraded`
- `exit_code`
- `started_at`
- `duration_ms`

## 参数

```text
speedtest [options]

  --dl-url URL
  --ul-url URL
  --latency-url URL
  --max SIZE
  --timeout SECONDS
  --threads N
  --latency-count N
  --lang LANG
  --json
  --non-interactive
  --endpoint IP
  --no-metadata
  -h, --help
  -v, --version
```

说明：

- TTY 下默认会展示候选节点并允许手动选择。
- `--non-interactive` 会禁用交互并自动选择最快的健康节点。
- `--endpoint` 会跳过节点发现，直接固定到指定 IP。
- `--no-metadata` 会跳过客户端 / 服务端 ASN 与地理信息查询。
- 当 `DL_URL`、`UL_URL`、`LATENCY_URL` 主机不一致时，会禁用共享节点固定并返回降级告警。

## 退出码

- `0`: 全部成功
- `1`: 启动失败或配置错误
- `2`: 测速完成，但有降级或部分阶段失败
- `130`: 用户中断

## 网络依赖

- Apple CDN: `https://mensura.cdn-apple.com`
- Cloudflare DoH: `https://cloudflare-dns.com`
- AliDNS DoH: `https://dns.alidns.com`
- ip-api: `http://ip-api.com`

其中：

- DoH 用于候选节点发现
- ip-api 只用于 best-effort 元数据补充，不影响主测速流程

## 构建与开发

要求：

- Go `1.25+`

本地运行：

```bash
go run ./cmd/speedtest
```

质量检查：

```bash
bash scripts/check.sh
```

构建 Release 归档：

```bash
bash scripts/build.sh
```

产物：

- `speedtest-darwin-amd64.tar.gz`
- `speedtest-darwin-arm64.tar.gz`
- `speedtest-linux-amd64.tar.gz`
- `speedtest-linux-arm64.tar.gz`
- `speedtest-windows-amd64.zip`
- `checksums-sha256.txt`

## CI / CD

- CI: macOS / Linux / Windows，显式 Go 版本矩阵，覆盖 `gofmt`、`vet`、`test`、`race` 和 CLI smoke。
- Release: tag 触发，先跑测试和三平台 smoke，再产出归档包和 `sha256` 校验文件。
