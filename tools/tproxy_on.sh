#!/system/bin/sh
# 透明代理：把 com.mi.health 出站的 443 重定向到 bandai（不设系统代理）
UID=$(stat -c %u /data/data/com.mi.health 2>/dev/null)
PORT=${1:-8081}

[ -z "$UID" ] && { echo "!! cannot get uid"; exit 1; }

# 清理旧规则
iptables -t nat -D OUTPUT -m owner --uid-owner $UID -j BANDAI 2>/dev/null
iptables -t nat -F BANDAI 2>/dev/null
iptables -t nat -X BANDAI 2>/dev/null

# 建链
iptables -t nat -N BANDAI || { echo "!! cannot create chain"; exit 1; }

# 回环必须放行，否则 bandai 自己连自己会死循环
iptables -t nat -A BANDAI -d 127.0.0.1 -j RETURN

# 只劫持 443（HTTP 不动，避免干扰）
iptables -t nat -A BANDAI -p tcp --dport 443 -j REDIRECT --to-ports $PORT

# 只对目标 App 的 uid 生效，其他 App 完全不受影响
iptables -t nat -A OUTPUT -m owner --uid-owner $UID -j BANDAI

echo "uid=$UID port=$PORT"
echo "--- BANDAI chain ---"
iptables -t nat -L BANDAI -n -v 2>/dev/null
echo "--- OUTPUT hook ---"
iptables -t nat -L OUTPUT -n 2>/dev/null | grep BANDAI