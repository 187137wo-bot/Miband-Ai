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

| 服务 | Base URL | 备注 |
|---|---|---|
| 小米 MiMo | `https://api.xiaomimimo.com/v1/chat/completions` | 小米官方，计费 |
| DeepSeek | `https://api.deepseek.com/chat/completions` | 高峰期可能 503 |
| 硅基流动 | `https://api.siliconflow.cn/v1/chat/completions` | 多模型可选 |
| 月之暗面 | `https://api.moonshot.cn/v1/chat/completions` | Kimi |

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

## ⚠️ 免责声明

- 本项目仅供学习研究使用
- 使用第三方 API 产生的费用由用户自行承担
- 修改系统证书和 iptables 规则有一定风险，请确保了解操作后果
- 作者不对因使用本项目导致的任何问题负责

## 📄 License

MIT License
