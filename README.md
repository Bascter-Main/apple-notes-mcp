# iCloud Notes MCP

[English](README_EN.md)

一个将 Apple「备忘录」安全连接到 ChatGPT 的只读 MCP 服务。它在你的服务器上读取经白名单授权的 iCloud 备忘录，并通过 HTTPS 与 OAuth 向 ChatGPT 提供 MCP 工具，使 GPT 能够在对话中检索文件夹、笔记元数据和正文。

本项目不是 Apple 官方 SDK。底层适配器依赖未公开的 CloudKit 接口，Apple 更新服务后可能需要重新适配。

## 功能与边界

- 仅提供只读工具，不支持创建、修改或删除备忘录。
- `ALLOWED_FOLDER_IDS` 按文件夹 ID 限制访问范围，并自动包含其所有子文件夹。
- 支持列出文件夹、列出笔记、按时间查询变更、批量读取正文和检查同步状态。
- 不返回附件下载地址或图片二进制内容。
- Apple ID、密码和两步验证代码只在 VPS 的 SSH 终端中输入，不会发送给 ChatGPT。
- 公网请求先经过 OAuth 网关；MCP 核心、OAuth 服务和管理接口均只监听本机回环地址。
- 当前为单用户部署：网关只允许一个指定的 OAuth 用户访问。

## 架构

```text
ChatGPT
   │  HTTPS + OAuth access token
   ▼
Nginx :443
   ▼
notes-gateway 127.0.0.1:8766
   │  验证签名、签发者、资源、有效期、notes:read 和用户身份
   ▼
notes-mcp 127.0.0.1:8765
   │  强制执行文件夹白名单和只读工具
   ▼
iCloud Notes / CloudKit

Authplane 127.0.0.1:9000  OAuth 服务
Authplane 127.0.0.1:9001  管理接口，不对公网开放
```

只有 Nginx 对公网监听。请勿开放 `8765`、`8766`、`9000` 或 `9001`，也不要让 Nginx 绕过网关直接代理到 `8765`。

## 部署

以下流程面向使用 systemd 的 Linux VPS，推荐 Debian 或 Ubuntu。部署脚本采用分阶段、失败即关闭的顺序：OAuth 网关通过本机拒绝测试前，公网 `/mcp` 始终保持不可访问。

### 1. 准备服务器、域名和 TLS

需要：

- 一台 `x86_64` 或 `arm64` Linux VPS；
- 一个已解析到该 VPS 的域名，例如 `mcp.example.com`；
- Nginx 和位于 `/etc/letsencrypt/live/<域名>/` 的有效证书；
- Git、Python 3、curl、OpenSSL、tar、unzip 和 Go 1.26 或更高版本；
- 服务器能够访问 iCloud、Apple 身份验证服务、GitHub 和 Go 模块下载服务。

Debian/Ubuntu 可先安装基础软件：

```bash
sudo apt-get update
sudo apt-get install -y nginx git python3 curl openssl tar unzip ca-certificates
```

Go 版本必须满足项目要求；如系统软件源版本过旧，请从 [go.dev/dl](https://go.dev/dl/) 安装。TLS 证书也需要在继续之前准备完成，例如使用 Certbot：

```bash
sudo apt-get install -y certbot python3-certbot-nginx
sudo certbot certonly --nginx -d mcp.example.com
```

防火墙只需允许 SSH、HTTP 和 HTTPS。不得向公网开放 `8765`、`8766`、`9000`、`9001`。

### 2. 下载项目并设置域名

```bash
git clone https://github.com/Bascter-Main/icloud-notes-mcp.git
cd icloud-notes-mcp
cp .env.example .env
```

编辑 `.env`：

```dotenv
PUBLIC_HOST=mcp.example.com
AUTHPLANE_ISSUER=https://mcp.example.com
```

加载配置：

```bash
set -a
. ./.env
set +a
```

`PUBLIC_HOST` 只填写主机名，不带 `https://`；`AUTHPLANE_ISSUER` 填写对应的 HTTPS 源。不要提交 `.env`。

### 3. 构建并安装 MCP 核心

```bash
cd mcp
bash scripts/bootstrap.sh
sudo bash scripts/install-vps.sh
```

`bootstrap.sh` 会获取固定版本的 `torkve/icloud-notes`、应用项目补丁、运行测试并构建 `notes-mcp` 与 `notesctl`。安装脚本只安装文件和 systemd 服务，此时不会启动 MCP。

### 4. 登录 iCloud 并设置文件夹白名单

先在 SSH 终端登录：

```bash
sudo -u notesmcp /opt/icloud-notes-mcp/bin/notesctl \
  -action login -state-dir /var/lib/notes-mcp
```

按提示输入 Apple ID、密码和六位验证码。不要把这些信息写入配置文件、发送给 ChatGPT 或提交到 GitHub。

列出 iCloud 文件夹名称与 ID：

```bash
sudo -u notesmcp /opt/icloud-notes-mcp/bin/notesctl \
  -action folders -state-dir /var/lib/notes-mcp
```

建议先创建一个不含敏感信息的测试文件夹，只授权该文件夹。编辑配置：

```bash
sudoedit /etc/notes-mcp/config.env
```

至少修改：

```dotenv
ALLOWED_FOLDER_IDS=你的文件夹ID
```

多个根文件夹用英文逗号分隔。每个根文件夹会自动包含全部子文件夹。白名单为空、ID 不存在或扫描数量超过 `MAX_SCAN_NOTES` 时，服务会拒绝返回数据，而不会放宽范围。

可选：只统计某个根文件夹中的笔记，不输出正文：

```bash
sudo -u notesmcp /opt/icloud-notes-mcp/bin/notesctl \
  -action probe -root '文件夹ID' -state-dir /var/lib/notes-mcp
```

启动并检查 MCP 核心：

```bash
sudo systemctl enable --now notes-mcp
sudo systemctl status notes-mcp --no-pager
curl -fsS http://127.0.0.1:8765/healthz
```

正常响应为 `alive`。

### 5. 安装 OAuth 服务

返回仓库根目录，并确保第 2 步中的环境变量仍然有效：

```bash
cd ..
sudo PUBLIC_HOST="$PUBLIC_HOST" bash scripts/install-authplane-vps-stage1.sh
```

脚本会安装并校验 Authplane v0.2.3，生成独立随机密钥，并将 OAuth 服务和管理接口分别限制在 `127.0.0.1:9000` 与 `127.0.0.1:9001`。此步骤不会修改 Nginx，也不会开放 MCP。

### 6. 创建 OAuth 资源和唯一授权用户

```bash
sudo AUTHPLANE_ISSUER="$AUTHPLANE_ISSUER" \
  python3 scripts/01-provision.py
```

按提示创建登录邮箱、显示名称和至少 16 位的 OAuth 密码。这里创建的是本机 OAuth 账户，不是 Apple 账户。脚本会创建 `notes:read` 资源，并把该用户 ID 写入仅 root 可读的网关配置。

### 7. 对外发布 OAuth，保持 MCP 阻断

```bash
sudo PUBLIC_HOST="$PUBLIC_HOST" \
  bash scripts/02-expose-oauth-still-block-mcp.sh
```

完成后，OAuth Discovery 应返回 `200`，公网 `/mcp` 仍应返回 `403`。

### 8. 安装 OAuth 网关

```bash
sudo PUBLIC_HOST="$PUBLIC_HOST" bash scripts/03-install-gateway.sh
```

脚本会构建并启动 `notes-gateway`，验证本机无令牌和无效令牌请求均被拒绝。此时公网 `/mcp` 仍为 `403`。

### 9. 激活受保护的 MCP 入口

```bash
sudo PUBLIC_HOST="$PUBLIC_HOST" bash scripts/04-activate-protected-mcp.sh
```

脚本只替换 Nginx 中预先验证过的 `/mcp` 阻断配置。激活成功后，公网无令牌请求必须返回 `401`，并带有 `WWW-Authenticate: Bearer`。若检查失败，脚本会恢复先前的 `403` 配置。

### 10. 验收部署

```bash
curl -sS -o /dev/null -w 'MCP no token: %{http_code}\n' \
  -X POST "https://${PUBLIC_HOST}/mcp" \
  -H 'Content-Type: application/json' -d '{}'

curl -sS -o /dev/null -w 'OAuth discovery: %{http_code}\n' \
  "https://${PUBLIC_HOST}/.well-known/oauth-authorization-server"

curl -sS -o /dev/null -w 'Admin API: %{http_code}\n' \
  "https://${PUBLIC_HOST}/admin/users"

sudo ss -lntp \
  '( sport = :8765 or sport = :8766 or sport = :9000 or sport = :9001 )'
```

预期结果：

- `MCP no token: 401`
- `OAuth discovery: 200`
- `Admin API: 403`
- 四个内部端口均只监听 `127.0.0.1`

### 11. 连接 ChatGPT

1. 在 ChatGPT 网页端打开 **Plugins**。
2. 点击加号，选择 **Add custom MCP server**。
3. 填写名称，例如 `iCloud Notes`。
4. Server URL 填写 `https://你的域名/mcp`。
5. Authentication 选择 **OAuth**，并使用服务器公布的 CIMD 配置。
6. 阅读风险提示后创建插件，在个人插件中安装。
7. 回到对话，在输入框键入 `@` 并选择该插件。
8. 首次调用工具时，使用第 6 步创建的 OAuth 账户登录并授权 `notes:read`。

这里输入的是 OAuth 账户，绝不是 Apple ID 或 Apple 密码。ChatGPT 当前的自定义 MCP 连接步骤可参考 [OpenAI 官方文档](https://developers.openai.com/api/docs/guides/custom-mcp-server)。界面入口和工作区权限可能随账户方案或管理员策略而不同。

连接后可先要求 ChatGPT 调用 `get_sync_status`，再使用 `list_folders`、`list_notes` 和 `get_notes_batch` 验证可见范围与正文读取。测试时不要让模型在回复中复述敏感笔记。

## 配置

MCP 核心配置位于 `/etc/notes-mcp/config.env`：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `MCP_BIND` | `127.0.0.1:8765` | MCP 核心监听地址，不应改为公网地址 |
| `ICLOUD_STATE_DIR` | `/var/lib/notes-mcp` | 加密会话保存目录 |
| `ALLOWED_FOLDER_IDS` | 无 | 允许读取的根文件夹 ID，多个值用逗号分隔 |
| `MAX_SCAN_NOTES` | `1000` | 单次扫描的安全上限 |
| `ICLOUD_READ_TIMEOUT` | `90s` | iCloud 读取超时 |
| `MCP_CACHE_SECONDS` | `120` | 内存缓存时间；重启后清空 |

修改后重启服务：

```bash
sudo systemctl restart notes-mcp
curl -fsS http://127.0.0.1:8765/healthz
```

## 更新与排错

查看服务日志：

```bash
sudo journalctl -u notes-mcp -n 100 --no-pager
sudo journalctl -u notes-gateway -n 100 --no-pager
sudo journalctl -u authplane -n 100 --no-pager
```

会话失效时重新运行 `notesctl -action login`。Apple 可能限制高频登录，请勿连续重试。不要在 Issue 或日志片段中公开 Apple 凭据、Cookie、会话文件、OAuth 令牌、真实笔记标题或正文。

每个修改 Nginx 的脚本都会在 `/root/notes-mcp-backups/` 创建带时间戳的备份。需要回滚时，恢复备份的 `notes-mcp.conf`，运行 `nginx -t`，再重新加载 Nginx。

## 本地构建验证

在 Linux 环境中从仓库根目录运行：

```bash
bash scripts/test-public-package.sh
```

脚本会检查 Shell 与 Python 文件、测试和构建网关，并在临时目录中获取固定版本的上游代码、运行带 `icloud` 标签的测试并构建 MCP 二进制文件。

## 已知限制

- iCloud 适配依赖未公开的 CloudKit 行为，兼容性无法由 Apple 保证。
- 保存的 Apple 会话具有账户级能力；“只读”由本项目服务端执行，不能消除 VPS 泄露会话后的风险。
- 仅提取文本内容；富文本细节、清单状态、附件和图片 OCR 不保证保留。
- 修改时间不等于内容最初产生时间；本项目也不会保存被覆盖的历史版本。
- 已签发的短期访问令牌在到期前可能不会因账户撤销而立即失效。

更详细的安全说明见 [`docs/SECURITY.md`](docs/SECURITY.md)。

## 许可证

项目代码采用 [MIT License](LICENSE)。第三方组件保留各自许可证，详见 [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md)。
