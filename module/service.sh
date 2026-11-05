#!/system/bin/sh
# late_start service: zygote 已就绪。
# 策略：
#   1) 不设系统代理（App 会检测到并禁用语音）
#   2) 用 iptables 按 uid 把 com.mi.health 的 443 重定向到 bandai 的透明端口
#   3) 用 ip6tables REJECT IPv6 443，强制 App 回落 IPv4
#   4) bandai 确认存活后才装规则，一退出立刻卸规则
MODDIR=${0%/*}
BASE=/data/adb/band-ai
HASH=dde4768f
APEX=/apex/com.android.conscrypt/cacerts
FULL=$BASE/cacerts_full
LOG=$BASE/logs/service.log
PKG=com.mi.health
TPORT=8081

log() { echo "[$(date +%H:%M:%S)] $*" >> "$LOG"; }

log "=== bandai service start ==="

# 等开机完成
i=0
while [ "$(getprop sys.boot_completed)" != "1" ] && [ $i -lt 120 ]; do
    sleep 1; i=$((i+1))
done
log "boot_completed after ${i}s"

# ---- 1. APEX + system cacerts bind ----
SYSC=/system/etc/security/cacerts
if [ -d "$FULL" ]; then
    for Z in $(pidof zygote64) $(pidof zygote); do
        [ -z "$Z" ] && continue
        if [ -d "$APEX" ]; then
            nsenter --mount=/proc/$Z/ns/mnt -- mount --bind "$FULL" "$APEX" 2>/dev/null \
                && log "bind apex ok (zygote $Z)" || log "bind apex FAILED (zygote $Z)"
        fi
        if [ -d "$SYSC" ]; then
            nsenter --mount=/proc/$Z/ns/mnt -- mount --bind "$FULL" "$SYSC" 2>/dev/null \
                && log "bind system cacerts ok (zygote $Z)" || log "bind system cacerts FAILED (zygote $Z)"
        fi
    done
    sleep 2
    am force-stop com.mi.health 2>/dev/null
    log "force-stopped com.mi.health to refresh mount view"
else
    log "skip cacerts bind (full=$([ -d $FULL ] && echo yes || echo no))"
fi

chmod 755 "$MODDIR/bin/bandai" 2>/dev/null

# ---- 2. iptables / ip6tables 规则管理 ----
tproxy_uid() {
    stat -c %u /data/data/$PKG 2>/dev/null
}

tproxy_on() {
    UID=$(tproxy_uid)
    [ -z "$UID" ] && { log "!! no uid for $PKG"; return 1; }

    # IPv4
    iptables -t nat -D OUTPUT -m owner --uid-owner $UID -j BANDAI 2>/dev/null
    iptables -t nat -F BANDAI 2>/dev/null
    iptables -t nat -X BANDAI 2>/dev/null
    iptables -t nat -N BANDAI 2>/dev/null || return 1
    iptables -t nat -A BANDAI -d 127.0.0.1 -j RETURN 2>/dev/null
    iptables -t nat -A BANDAI -p tcp --dport 443 -j REDIRECT --to-ports $TPORT 2>/dev/null
    iptables -t nat -A OUTPUT -m owner --uid-owner $UID -j BANDAI 2>/dev/null

    # IPv6: REJECT 强制回落 IPv4
    ip6tables -D OUTPUT -m owner --uid-owner $UID -p tcp --dport 443 -j REJECT --reject-with tcp-reset 2>/dev/null
    ip6tables -A OUTPUT -m owner --uid-owner $UID -p tcp --dport 443 -j REJECT --reject-with tcp-reset 2>/dev/null

    log "tproxy ON uid=$UID port=$TPORT (v4+v6)"
    return 0
}

tproxy_off() {
    UID=$(tproxy_uid)
    [ -n "$UID" ] && {
        iptables -t nat -D OUTPUT -m owner --uid-owner $UID -j BANDAI 2>/dev/null
        iptables -t nat -F BANDAI 2>/dev/null
        iptables -t nat -X BANDAI 2>/dev/null
        ip6tables -D OUTPUT -m owner --uid-owner $UID -p tcp --dport 443 -j REJECT --reject-with tcp-reset 2>/dev/null
    }
    log "tproxy OFF"
}

# 开机先清残留
tproxy_off
settings put global http_proxy :0 2>/dev/null
settings delete global http_proxy 2>/dev/null

# ---- 2.5 独立看守：脱离本脚本生命周期，bandai 挂了也能清残留 ----
if [ -f "$MODDIR/guard.sh" ]; then
    chmod 755 "$MODDIR/guard.sh" 2>/dev/null
    # 先杀掉旧的看守（重装/重启时可能有残留）
    pkill -f "guard.sh" 2>/dev/null
    sleep 1
    if command -v setsid >/dev/null 2>&1; then
        setsid "$MODDIR/guard.sh" < /dev/null > /dev/null 2>&1 &
    else
        nohup "$MODDIR/guard.sh" < /dev/null > /dev/null 2>&1 &
    fi
    sleep 1
    if pidof -s guard.sh >/dev/null 2>&1 || pgrep -f guard.sh >/dev/null 2>&1; then
        log "guard spawned"
    else
        log "!! guard failed to spawn"
    fi
fi

# ---- 3. 守护：bandai 活着才装规则，退出立刻卸 ----
(
    trap '' HUP TERM
    while true; do
        # 关键防护：每轮启动前强制清掉系统代理。
        # 如果代理残留指向 127.0.0.1:8080 而 bandai 没在跑，
        # 所有走系统代理的 App 会全部断网（浏览器/抖音等）。
        settings put global http_proxy :0 2>/dev/null
        settings delete global http_proxy 2>/dev/null

        "$MODDIR/bin/bandai" >> "$BASE/logs/bandai.log" 2>&1 &
        BPID=$!
        echo "$BPID" > "$BASE/bandai.pid"

        sleep 3
        if kill -0 "$BPID" 2>/dev/null; then
            log "bandai up pid=$BPID"
            tproxy_on || log "!! tproxy_on failed"
        else
            log "!! bandai failed to start, tproxy NOT set"
        fi

        wait "$BPID"
        tproxy_off
        settings put global http_proxy :0 2>/dev/null
        settings delete global http_proxy 2>/dev/null
        log "!! bandai exited, tproxy+proxy cleared, retry in 5s"
        sleep 5
    done
) &

log "watchdog spawned"
log "=== bandai service done ==="
