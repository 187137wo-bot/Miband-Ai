#!/system/bin/sh
# 立刻撤销透明代理，恢复 App 直连
UID=$(stat -c %u /data/data/com.mi.health 2>/dev/null)

iptables -t nat -D OUTPUT -m owner --uid-owner $UID -j BANDAI 2>/dev/null
iptables -t nat -F BANDAI 2>/dev/null
iptables -t nat -X BANDAI 2>/dev/null

# 双保险：确保系统代理也是空的
settings put global http_proxy :0 2>/dev/null
settings delete global http_proxy 2>/dev/null

echo "tproxy cleared (uid=$UID)"
iptables -t nat -L OUTPUT -n 2>/dev/null | grep -c BANDAI