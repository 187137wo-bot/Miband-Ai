#!/system/bin/sh
# Magisk/KSU 模块安装脚本

SKIPUNZIP=0

ui_print "- BandAI 模块安装中..."

# 创建运行时目录
mkdir -p /data/adb/band-ai/logs
mkdir -p /data/adb/band-ai/ca

# 如果配置文件不存在，从模板复制
if [ ! -f /data/adb/band-ai/config.json ]; then
    cp "$MODPATH/config.template.json" /data/adb/band-ai/config.json
    ui_print "- 已创建默认配置，请打开 http://127.0.0.1:8082 填写 API Key"
else
    ui_print "- 配置文件已存在，保留现有配置"
fi

# 生成 CA（如果不存在）
if [ ! -f /data/adb/band-ai/ca.crt ]; then
    ui_print "- CA 证书将在首次启动时自动生成"
fi

chmod 755 "$MODPATH/service.sh"
chmod 755 "$MODPATH/post-fs-data.sh"
chmod 755 "$MODPATH/bin/bandai" 2>/dev/null

ui_print "- 安装完成！"
ui_print "- 重启后生效"
ui_print "- 配置页面: http://127.0.0.1:8082"
