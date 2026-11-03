#!/system/bin/sh
# 卸载时清理

# 清除 iptables 规则
UID=$(stat -c %u /data/data/com.mi.health 2>/dev/null)
if [ -n "$UID" ]; then
    iptables -t nat -D OUTPUT -m owner --uid-owner $UID -j BANDAI 2>/dev/null
    iptables -t nat -F BANDAI 2>/dev/null
    iptables -t nat -X BANDAI 2>/dev/null
    ip6tables -D OUTPUT -m owner --uid-owner $UID -p tcp --dport 443 -j REJECT --reject-with tcp-reset 2>/dev/null
fi

# 清除系统代理
settings put global http_proxy :0 2>/dev/null
settings delete global http_proxy 2>/dev/null

# 杀掉 bandai 进程
pkill -9 -f bin/bandai 2>/dev/null

# 注意：不删除 /data/adb/band-ai/config.json，保留用户配置方便重装
