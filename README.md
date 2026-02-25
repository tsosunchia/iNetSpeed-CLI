# Apple CDN Network Bench

> Apple CDN (`mensura.cdn-apple.com`) 的下载/上传/延迟测速脚本集合。

## 包含脚本

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

## 常用环境变量

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

## 节点选择逻辑（主脚本）

1. 通过 AliDNS DoH 查询 `mensura.cdn-apple.com` 的 A 记录。
2. 用 ip-api 查询每个 IP 的地域/ASN 信息。
3. 交互终端下可手动选择节点；非交互环境默认选择第 1 个。
4. 选中后通过 `curl --resolve host:443:IP` 固定后续测试连接目标。

## 许可证

本项目采用 **GNU General Public License v3.0 (GPL-3.0-or-later)** 开源。

- 许可证全文见 `LICENSE`。
