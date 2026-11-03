#!/system/bin/sh
# post-fs-data: 在 zygote 启动之前执行。
# 这是唯一能生效的时机 —— Java 层的 TrustedCertificateStore 在 zygote 里
# 建立缓存，App fork 时直接继承。等 zygote 起来再挂载，永远追不上。
MODDIR=${0%/*}
BASE=/data/adb/band-ai
HASH=dde4768f
LOG=$BASE/logs/service.log

mkdir -p "$BASE/logs"
log() { echo "[$(date +%H:%M:%S)] post-fs-data: $*" >> "$LOG"; }

# ---- 1. CA 与私钥 ----
if [ ! -f "$BASE/ca.crt" ] && [ -f "$MODDIR/ca/ca.crt" ]; then
    cp -f "$MODDIR/ca/ca.crt" "$BASE/ca.crt"
    cp -f "$MODDIR/ca/ca.key" "$BASE/ca.key"
    chmod 644 "$BASE/ca.crt"
    chmod 600 "$BASE/ca.key"
    log "ca installed"
fi

# ---- 2. 配置 ----
if [ ! -f "$BASE/config.json" ] && [ -f "$MODDIR/config.json" ]; then
    cp -f "$MODDIR/config.json" "$BASE/config.json"
    chmod 644 "$BASE/config.json"
fi

SYS=/system/etc/security/cacerts
APEX=/apex/com.android.conscrypt/cacerts
FULL=$BASE/cacerts_full

# ---- 3. 准备完整目录（原始证书 + 我们的） ----
if [ ! -d "$FULL" ]; then
    mkdir -p "$FULL"
    SRC=""
    [ -d "$SYS" ] && [ "$(ls "$SYS" 2>/dev/null | wc -l)" -gt 10 ] && SRC="$SYS"
    [ -z "$SRC" ] && [ -d "$APEX" ] && [ "$(ls "$APEX" 2>/dev/null | wc -l)" -gt 10 ] && SRC="$APEX"
    if [ -n "$SRC" ]; then
        cp -a "$SRC"/. "$FULL"/ 2>/dev/null
        log "seeded cacerts_full from $SRC ($(ls "$FULL" | wc -l) certs)"
    else
        log "!! no source cacerts dir found (sys=$([ -d $SYS ] && echo y || echo n) apex=$([ -d $APEX ] && echo y || echo n))"
    fi
    cp -f "$MODDIR/ca/$HASH.0" "$FULL/$HASH.0" 2>/dev/null
    chmod 644 "$FULL"/* 2>/dev/null
    chown root:root "$FULL"/* 2>/dev/null
    chcon -R u:object_r:system_security_cacerts_file:s0 "$FULL" 2>/dev/null
fi

# ---- 4. 关键：此时 zygote 还没起来，用 tmpfs 覆盖 system 的 cacerts ----
if [ -d "$SYS" ] && [ -d "$FULL" ] && [ "$(ls "$FULL" 2>/dev/null | wc -l)" -gt 10 ]; then
    if mount -t tmpfs tmpfs "$SYS" 2>/dev/null; then
        cp -a "$FULL"/. "$SYS"/ 2>/dev/null
        chmod 644 "$SYS"/* 2>/dev/null
        chown root:root "$SYS"/* 2>/dev/null
        chcon -R u:object_r:system_security_cacerts_file:s0 "$SYS" 2>/dev/null
        GOT=$(ls "$SYS" 2>/dev/null | wc -l)
        if [ "$GOT" -lt 100 ]; then
            # 回滚：证书残缺会让整机 HTTPS 全部失败，宁可装不上也不能这样
            umount "$SYS" 2>/dev/null
            log "!! rollback $SYS (only $GOT certs copied)"
        else
            log "tmpfs $SYS ok, $GOT certs"
        fi
    else
        log "!! tmpfs $SYS FAILED"
    fi
else
    log "skip tmpfs system (sys=$([ -d $SYS ] && echo y || echo n) full=$(ls "$FULL" 2>/dev/null | wc -l))"
fi
