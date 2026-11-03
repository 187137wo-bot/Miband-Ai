# 更新日志

## v1.3.0-beta.2 (2026-11-03) 🧪 测试版

> ⚠️ **测试版**：多轮记忆为新增功能，可能存在上下文污染、响应变慢等问题。遇到问题请回退到 v1.2。**默认关闭，不开启则行为与 v1.2 完全一致。**

### 修复
- 🐛 **配置页面卡死**：`GET /api/config` 误用全局写锁，导致点「测试连通性」期间（最长 10 秒）配置页面完全打不开。现已改为读操作不持锁
- 🐛 `/api/test` 同样去掉全局锁，测试期间页面可正常使用

---

## v1.3.0-beta.1 (2026-11-03) 🧪 测试版

> ⚠️ **测试版**：多轮记忆为新增功能，可能存在上下文污染、响应变慢等问题。遇到问题请回退到 v1.2。**默认关闭，不开启则行为与 v1.2 完全一致。**

### 新增
- 🧠 **多轮记忆**：短时间内连续提问会带上上文，能接住「它」「刚才那个」这类指代
- ⚙️ 记忆轮数可自定义（1~50，推荐 5-10）
- ⏱️ 记忆窗口可自定义（10~3600 秒，推荐 120-300）
- Web UI 新增「开启多轮记忆」开关，参数带推荐值提示

### 设计
- **按连接隔离**：每个 WebSocket 连接一份独立记忆，多设备不会串话
- **超时清空**：超过窗口时间自动丢弃上下文，避免话题污染
- **默认关闭**：不勾选则完全无记忆
- **只记成功问答**：API 返回空时不写入，避免污染上下文
- **轮数截断**：超过设定轮数只保留最近 N 轮，防止 token 爆炸

### 已知局限
- bandai 重启后记忆清零（内存存储，不持久化）
- 连接断开即丢失（若 App 每次提问都新建 WebSocket，记忆窗口实际不起作用）
- 手环 dialog_id 每次都不同，只能靠时间窗口近似会话

---

## v1.2 (2026-11-03)

### 新增
- **自动关闭思考模式**：请求里显式传 `thinking: {"type": "disabled"}` 和 `reasoning_effort: "none"`，推理模型（如 `deepseek-flash`）不再输出思考过程
- 新增配置项 `allow_thinking`（默认 false），需要思考链时可开启
- API 对未知参数报 400 时自动去掉这些参数重试

### 实测结果
| 参数 | 是否生效 |
|---|---|
| `thinking: {"type": "disabled"}` | ✅ 有效 |
| `reasoning_effort: "none"` | ✅ 有效 |
| `enable_thinking: false` | ❌ 无效 |
| `chat_template_kwargs` | ❌ 无效 |

### 修复
- API URL 自动补全逻辑同步到 Web UI 测试接口（之前测试时不补全会 404）

---

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
