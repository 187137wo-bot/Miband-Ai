# 更新日志

## v1.1 (2026-11-03)

### 新增
- **Web 管理界面**：浏览器打开 `http://127.0.0.1:8082` 即可配置 API Key、模型等，无需改文件
- **测试连通性**：Web UI 内置一键测试，验证 API 配置是否正确
- **历史预设**：保存多套配置（如 MiMo / DeepSeek），一键切换
- **保存自动重启**：Web UI 保存配置后 bandai 自动重启，约 5 秒生效
- **API URL 自动补全**：填 base URL（如 `https://api.xxx.com/v1`）自动追加 `/chat/completions`

### 修复
- **WebSocket 分片帧**：长回答被拆成多个 fragment 帧时，现在会正确拼接后再改写
- **推理模型兼容**：支持 `deepseek-flash` 等推理模型的 `reasoning_content` 字段
- **多种响应格式**：兼容 content 为字符串、数组、delta 三种格式
- **小米回答覆盖**：bandai 改写成功后，丢弃小米服务器后续发的 Toast 帧，防止覆盖
- **uninstall.sh**：卸载时清理 iptables / ip6tables 规则
- **service.sh**：改用 iptables 透明代理 + ip6tables IPv6 REJECT，不再设系统代理

### 改进
- 日志增加 `dialog_id`，方便排查问题
- API 调用增加详细错误日志（原始响应体）
- 连接级 5 分钟超时，防止 goroutine 泄漏

---

## v1.0 (2026-11-03)

### 首次发布
- iptables 透明代理劫持小米运动健康 App 的语音流量
- TLS MITM + SNI 识别 `speech.ai.xiaomi.com`
- WebSocket 帧解析，改写 `Template.Toast` 回答
- 支持任意 OpenAI 兼容 API（MiMo / DeepSeek / 硅基流动等）
- Magisk / KernelSU 模块，开机自启
- post-fs-data tmpfs 挂载 CA 证书
