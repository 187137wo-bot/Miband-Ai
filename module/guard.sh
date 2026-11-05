#!/system/bin/sh
# 独立看守进程：不依赖 bandai 存活，监控代理和 iptables 残留。
# 一旦发现 bandai 已死但代理/规则还在，立刻清理，防止整机断网。
# 由 service.sh 用 setsid 启动，脱离父进程生命周期。

BASE=/data/adb/band-ai
LOG=$BASE/logs/guard.log
PKG=com.mi.health

mkdir -p "$BASE/logs" 2>/dev/null
log() { echo "[$(date +%H:%M:%S)] $*" >> "$LOG"; }

# 日志超过 100KB 就截断，防止无限增长
if [ -f "$LOG" ] && [ "$(wc -c < "$LOG" 2>/dev/null || echo 0)" -gt 102400 ]; then
    tail -500 "$LOG" > "$LOG.tmp" 2>/dev/null && mv "$LOG.tmp" "$LOG"
fi

log "=== guard started (pid $$) ==="

alive_bandai() {
    pidof bandai >/dev/null 2>&1
}

clean_proxy() {
    P=$(settings get global http_proxy 2>/dev/null)
    case "$P" in
        "127.0.0.1:8080"|"127.0.0.1:8081")
            if ! alive_bandai; then
                settings put global http_proxy :0 2>/dev/null
                settings delete global http_proxy 2>/dev/null
                log "!! cleared stale proxy=$P (bandai dead)"
                return 0
            fi
            ;;
    esac
    return 1
}

clean_iptables() {
    if alive_bandai; then
        return 1
    fi
    if ! iptables -t nat -L BANDAI -n >/dev/null 2>&1; then
        return 1
    fi
    UID=$(stat -c %u /data/data/$PKG 2>/dev/null)
    if [ -n "$UID" ]; then
        iptables -t nat -D OUTPUT -m owner --uid-owner $UID -j BANDAI 2>/dev/null
    fi
    iptables -t nat -F BANDAI 2>/dev/null
    iptables -t nat -X BANDAI 2>/dev/null
    log "!! cleared stale iptables BANDAI chain (bandai dead)"
    return 0
}

# 首次启动先做一次清理，处理上次崩溃的残留
clean_proxy
clean_iptables

COUNT=0
while true; do
    clean_proxy
    clean_iptables

    # 每 6 次（约 1 分钟）打一次心跳，方便确认看守还活着
    COUNT=$((COUNT+1))
    if [ $((COUNT % 6)) -eq 0 ]; then
        if alive_bandai; then
            log "guard alive, bandai ok"
        else
            log "guard alive, bandai NOT running (waiting for service.sh)"
        fi
    fi

    sleep 10
done
