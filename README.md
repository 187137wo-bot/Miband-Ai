# BandAI

将小米手环的小爱同学替换为自定义大模型（MiMo / DeepSeek / 任意 OpenAI 兼容 API）。

通过 iptables 透明代理 + WebSocket MITM 拦截手环语音请求，调用你自己的 AI API，改写回答后返回给手环显示。

## ✨ 特性

- 🔒 **透明代理**：App 无感知，不触发代理检测降级
- 🌐 **内置 Web UI**：浏览器打开 `http://127.0.0.1:8082` 即可配置 API Key、模型等
- 🔄 **自动守护**：bandai 崩溃自动重启，iptables 规则自动恢复
- 📱 **开机自启**：Magisk/KSU 模块，重启手机自动生效
- 🎯 **精准拦截**：只劫持语音流量，不影响其他网络请求
- 🔊 **音频处理**：自动丢弃 TTS 音频帧，避免冲突

## 📋 支持设备

| 设备 | 状态 |
|---|---|
| 小米手环 10 NFC | ✅ 已验证 |
| 小米手环 9 NFC | ⚠️ 待验证（协议大概率相同） |
| 小米手环 8 NFC | ⚠️ 待验证 |
| 更早型号 | ❌ 未测试 |

**手机要求**：任意品牌 Android 手机，已 Root（Magisk / KernelSU / APatch），安装了小米运动健康 App。

## 🚀 安装

### 前置条件

1. 手机已 Root（Magisk / KernelSU / APatch）
2. 已安装小米运动健康 App 并绑定手环
3. 手环语音功能正常可用

### 步骤

1. 下载最新 Release 的 `bandai-vX.X.zip`
2. 在 Magisk / KernelSU 中刷入模块
3. 重启手机
4. 手机浏览器打开 **`http://127.0.0.1:8082`**
5. 填入你的 API Key 和模型名，保存
6. 对手环说话测试

### 手动构建

```bash
# 需要 Go 1.21+
cd src/
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ../module/bin/bandai .
```

然后打包 `module/` 目录为 zip 刷入。

## ⚙️ 配置

打开 `http://127.0.0.1:8082`，可配置以下字段：

| 字段 | 说明 | 示例 |
|---|---|---|
| API URL | OpenAI 兼容的 chat/completions 完整地址 | `https://api.xiaomimimo.com/v1/chat/completions` |
| API Key | 你的 API 密钥 | `sk-xxx` |
| 模型名称 | 要使用的模型 | `mimo-v2.6-pro` |
| System Prompt | 系统提示词（建议简短） | 见默认值 |
| 最大回复字数 | 手环屏幕限制 | 60 |

### 推荐的 API 服务

| 服务 | Base URL | 推荐模型 | 备注 |
|---|---|---|---|
| 小米 MiMo | `https://api.xiaomimimo.com/v1` | `mimo-v2.6-pro` | 小米官方，计费 |
| DeepSeek | `https://api.deepseek.com` | `deepseek-chat` | 高峰期可能 503 |
| 硅基流动 | `https://api.siliconflow.cn/v1` | 见平台 | 多模型可选 |
| 月之暗面 | `https://api.moonshot.cn/v1` | `moonshot-v1-8k` | Kimi |

> **API URL 自动补全**：填 base URL（如 `https://api.deepseek.com`）即可，会自动追加 `/chat/completions`。

### ⚠️ 关于推理模型（已解决）

项目默认在请求里**显式关闭思考模式**，因此推理模型也能正常使用，回答不会带思考过程。

实测结果（DeepSeek）：

| 参数 | 是否生效 |
|---|---|
| `thinking: {"type": "disabled"}` | ✅ 有效 |
| `reasoning_effort: "none"` | ✅ 有效 |
| `enable_thinking: false` | ❌ 无效 |
| `chat_template_kwargs` | ❌ 无效 |

代码里会同时带上上述参数，API 会忽略不认识的；如果某个 API 对未知参数报 400，会自动去掉这些参数重试一次。

如需开启思考（不推荐，手环显示效果差），可在 `config.json` 里设置 `"allow_thinking": true`。

**推荐模型**：

| 场景 | 推荐 |
|---|---|
| 追求速度 | `deepseek-chat`、`mimo-v2.6-pro` |
| 追求能力 | `deepseek-flash`（已自动关闭思考，速度略慢） |

## 🔧 原理

```
手环 → 手机蓝牙 → 小米运动健康 App
                        ↓ (iptables REDIRECT by UID)
                   bandai (127.0.0.1:8081)
                        ↓ (TLS MITM + SNI 识别)
                   speech.ai.xiaomi.com ←→ bandai
                        ↓ (WebSocket 帧解析)
                   提取 query → 调用自定义 API → 改写 Template.Toast
                        ↓
                   手环显示改写后的回答
```

关键技术点：
- **iptables REDIRECT**：按 UID 劫持 `com.mi.health` 的 443 端口，App 完全无感知
- **ip6tables REJECT**：阻止 IPv6 连接，强制 App 回落 IPv4
- **SO_ORIGINAL_DST**：从内核获取原始目标 IP
- **SNI 解析**：从 TLS ClientHello 读取域名，精确识别 `speech.ai.xiaomi.com`
- **WebSocket 改写**：解析 RFC6455 帧，替换 `Template.Toast` 中的回答文本
- **post-fs-data tmpfs**：在 zygote 启动前覆盖系统证书存储，使 MITM 证书被信任

## 📁 项目结构

```
├── src/                     # Go 源码
│   ├── main.go              # 入口，启动代理 + 管理界面
│   ├── proxy.go             # CONNECT/MITM/隧道/WebSocket 改写
│   ├── transparent.go       # iptables 透明代理 + SNI 解析
│   ├── ws.go                # RFC6455 WebSocket 帧编解码
│   ├── ca.go                # CA 加载 + 动态签发叶子证书
│   ├── config.go            # 配置加载
│   └── admin.go             # Web 管理界面
├── module/                  # Magisk/KSU 模块
│   ├── module.prop          # 模块元信息
│   ├── post-fs-data.sh      # 开机挂载 CA 证书
│   ├── service.sh           # 开机启动 bandai + iptables
│   ├── customize.sh         # 安装脚本
│   ├── uninstall.sh         # 卸载清理
│   ├── config.template.json # 配置模板
│   ├── bin/                 # 预编译二进制
│   └── ca/                  # CA 生成脚本
├── tools/                   # 辅助脚本
│   ├── tproxy_on.sh         # 手动启用 iptables
│   └── tproxy_off.sh        # 手动禁用 iptables
├── build.sh                 # 一键构建
├── LICENSE
└── README.md
```

## 🚨 故障排查

### 突然没网了（最紧急）

**症状**：手机能连 WiFi/流量，但大部分 App 打不开，只有个别 App（如 B 站）能用。

**原因**：系统 HTTP 代理残留指向 `127.0.0.1:8080`，但 bandai 已被系统杀掉，没有进程监听这个端口，导致所有走系统代理的 App 全部断网。

**急救**（手机终端执行，需要 root）：

```sh
su -c "settings put global http_proxy :0"
su -c "settings delete global http_proxy"
```

**不用终端也行**：
- WiFi：设置 → WLAN → 长按当前网络 → 修改网络 → 高级 → 代理 → 改为「无」
- 移动数据：设置 → 移动网络 → 接入点(APN) → 检查「代理」字段为空

**预防**：内置 `guard.sh` 独立看守进程，每 10 秒检查，发现 bandai 已死但代理/iptables 残留就自动清理。

### 手环还是小爱在回答

```sh
# 1. bandai 是否在跑
su -c "ps -A | grep bandai"

# 2. iptables 规则是否存在
su -c "iptables -t nat -L BANDAI -n -v"

# 3. 规则命中计数为 0 说明流量没进来（多半是 App 走 IPv6）
su -c "ip6tables -L OUTPUT -n -v"
```

### API 报错

```sh
su -c "tail -30 /data/adb/band-ai/logs/bandai.log"
```

| 日志 | 原因 | 解决 |
|---|---|---|
| `api 401` | API Key 无效 | Web UI 重新填 |
| `api 503` | 服务端过载 | 换模型或等一会儿 |
| `api empty content` | 推理模型答案在 reasoning_content | 已自动关闭思考；仍出现请换非推理模型 |
| `empty reply` | API 返回空 | 点 Web UI「测试连通性」排查 |

### 配置页面打不开

```sh
su -c "netstat -tlnp | grep 8082"
su -c "ps -A | grep bandai"
```

bandai 没跑就手动启动：

```sh
su -c "setsid /data/adb/modules/bandai/bin/bandai >> /data/adb/band-ai/logs/bandai.log 2>&1 &"
```

### 彻底卸载

```sh
# 1. 清代理
su -c "settings put global http_proxy :0; settings delete global http_proxy"

# 2. 清 iptables
su -c "iptables -t nat -F BANDAI; iptables -t nat -X BANDAI"

# 3. 杀进程
su -c "pkill -9 -f bandai; pkill -9 -f guard.sh"

# 4. 删模块（重启后生效）
su -c "rm -rf /data/adb/modules/bandai"

# 5. 重启手机
```

---

## ⚠️ 免责声明

- 本项目仅供学习研究使用
- 使用第三方 API 产生的费用由用户自行承担
- 修改系统证书和 iptables 规则有一定风险，请确保了解操作后果
- 作者不对因使用本项目导致的任何问题负责

## 📄 License

MIT License
