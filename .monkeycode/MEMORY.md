# User Instruction Memory

This file records user instructions, preferences, and teachings for reference in future interactions.

## Format

### User Instruction Entry
User instruction entries should follow this format:

[User Instruction Summary]
- Date: [YYYY-MM-DD]
- Context: [Mentioned scenario or time]
- Instructions:
  - [Content of user teaching or instruction, described line by line]

### Project Knowledge Entry
Entries discovered by the Agent during task execution should follow this format:

[Project Knowledge Summary]
- Date: [YYYY-MM-DD]
- Context: Discovered by Agent while performing [specific task description]
- Category: [Operations & Deployment|Build Methods|Testing Methods|Troubleshooting & Debugging|Workflow & Collaboration|Environment Configuration]
- Instructions:
  - [Specific knowledge points, described line by line]

## Deduplication Strategy
- Before adding a new entry, check for similar or identical instructions.
- If a duplicate is found, skip the new entry or merge it with the existing one.
- When merging, update the context or date information.
- This helps avoid redundant entries and keeps the memory file tidy.

## Entries

[Project Knowledge Summary]
- Date: 2026-08-12
- Context: Discovered by Agent while pushing the cfip repository to GitHub
- Category: Environment Configuration
- Instructions:
  - GitHub 仓库 `nongsp/cfip` 的 `.git/config` 中配置了 `credential.helper=/app/agent/bin/agent git-credential-helper` 且 `credential.usehttppath=true`，该 agent helper 返回的 username 格式不受 GitHub 支持，导致 `git push` 报 "Invalid username or token"。
  - 解决办法：用 `git remote set-url origin https://x-access-token:<TOKEN>@github.com/nongsp/cfip` 将凭证内嵌到 origin URL（token 从 `git credential fill` 或 `/root/.netrc` 获取，`x-access-token` 为固定用户名）。
  - GitHub App installation token（`ghs_`）有效期短，`git push` 报 "Authentication failed" 时 token 已过期；用 `echo -e "protocol=https\nhost=github.com\n" | git credential fill` 从 credential helper 获取新 token，用 repos API 探测到 HTTP 200 后再更新 origin URL 与 `/root/.netrc`。
  - GitHub 网络偶发 `gnutls_handshake() failed` 瞬时 TLS 错误，重试 push 即可恢复。
  - Release workflow 必须配置 `on.push.tags: ['v*']`，仅 `branches` 过滤不会在打 tag 时触发 workflow。

[Project Knowledge Summary]
- Date: 2026-08-13
- Context: Discovered by Agent while diagnosing why cfip found no IPs on Windows while yx-tools did
- Category: Troubleshooting & Debugging
- Instructions:
  - Windows 默认临时端口池小于 Linux，且共享 keep-alive Transport 下高并发扫描大量短连接会堆满 TIME_WAIT 端口，表现为"扫不到任何 IP"；yx-tools 的 `-http` 方法用每 IP 独立 Transport + `SetLinger(0)` RST 关闭 + `CloseIdleConnections` 回收解决此问题。
  - cfip 已默认启用 `-http`（HTTPing）模式并用 HEAD 请求（完整 HTTP 交换含 TLS 握手与服务端响应，不传 body），`-http=false` 可回退旧 GET 模式。
  - 43.198.0.0/16（腾讯云香港段）用默认 domain `ipv4.svi.cc.cd` 实际可扫出 17~19 个有效 IP（200/403 混合，延迟 150~240ms），用于端到端验证 HTTPing 移植是否完整。
