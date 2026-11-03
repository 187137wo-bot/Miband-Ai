#!/system/bin/sh
# 卸载时恢复手机原状
BASE=/data/adb/band-ai

if [ -f "$BASE/bandai.pid" ]; then
    kill "$(cat "$BASE/bandai.pid")" 2>/dev/null
fi
pkill -f bandai 2>/dev/null

settings put global http_proxy :0 2>/dev/null
settings delete global http_proxy 2>/dev/null
settings delete global global_http_proxy_host 2>/dev/null
settings delete global global_http_proxy_port 2>/dev/null

rm -rf "$BASE"

# P4: 清除 iptables / ip6tables 规则
UID=$(stat -c %u /data/data/com.mi.health 2>/dev/null)
if [ -n "$UID" ]; then
    iptables -t nat -D OUTPUT -m owner --uid-owner $UID -j BANDAI 2>/dev/null
    iptables -t nat -F BANDAI 2>/dev/null
    iptables -t nat -X BANDAI 2>/dev/null
    ip6tables -D OUTPUT -m owner --uid-owner $UID -p tcp --dport 443 -j REJECT --reject-with tcp-reset 2>/dev/null
fi

echo "BandAI removed, proxy cleared, iptables cleaned."
