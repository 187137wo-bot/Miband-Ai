#!/system/bin/sh
# Magisk/KSU 模块安装脚本

SKIPUNZIP=0

ui_print "- BandAI 模块安装中..."

# 创建运行时目录
mkdir -p /data/adb/band-ai/logs
mkdir -p /data/adb/band-ai/ca

# 如果配置文件不存在，从模块内复制
if [ ! -f /data/adb/band-ai/config.json ]; then
    cp "$MODPATH/config.json" /data/adb/band-ai/config.json
    chmod 644 /data/adb/band-ai/config.json
    ui_print "- 已创建默认配置，请打开 http://127.0.0.1:8082 填写 API Key"
else
    ui_print "- 配置文件已存在，保留现有配置"
fi

# 安装 CA 证书到运行时目录
if [ -f "$MODPATH/ca/ca.crt" ]; then
    cp -f "$MODPATH/ca/ca.crt" /data/adb/band-ai/ca.crt
    cp -f "$MODPATH/ca/ca.key" /data/adb/band-ai/ca.key
    chmod 644 /data/adb/band-ai/ca.crt
    chmod 600 /data/adb/band-ai/ca.key
    ui_print "- CA 证书已安装"
fi

chmod 755 "$MODPATH/service.sh"
chmod 755 "$MODPATH/post-fs-data.sh"
chmod 755 "$MODPATH/guard.sh" 2>/dev/null
chmod 755 "$MODPATH/bin/bandai" 2>/dev/null

ui_print "- 安装完成！"
ui_print "- 重启后生效"
ui_print "- 配置页面: http://127.0.0.1:8082"
ui_print ""
ui_print "【紧急恢复】如果手机突然没网，终端执行："
ui_print "  su -c \"settings put global http_proxy :0\""
ui_print "  su -c \"settings delete global http_proxy\""
