#!/bin/bash
# BandAI 一键构建脚本
# 用法: ./build.sh [版本号]

set -e

VERSION=${1:-"v1.0"}
OUTDIR="dist"
MODDIR="module"
SRCDIR="src"

echo "=== BandAI Build ${VERSION} ==="

# 检查 Go
if ! command -v go &>/dev/null; then
    echo "错误: 未找到 go，请先安装 Go 1.21+"
    exit 1
fi

# 编译
echo "[1/4] 编译 bandai (linux/arm64)..."
cd "$SRCDIR"
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build \
    -trimpath -ldflags "-s -w" \
    -o "../${MODDIR}/bin/bandai" .
cd ..
echo "  -> $(ls -lh ${MODDIR}/bin/bandai | awk '{print $5}')"

# 生成 CA（如果不存在）
if [ ! -f "${MODDIR}/ca/ca.crt" ]; then
    echo "[2/4] 生成 CA 证书..."
    if command -v python3 &>/dev/null; then
        cd "${MODDIR}/ca"
        python3 gen_ca.py
        cd ../..
    else
        echo "  !! 跳过（需要 python3 + cryptography）"
        echo "  !! 请手动运行: cd module/ca && python3 gen_ca.py"
    fi
else
    echo "[2/4] CA 证书已存在，跳过"
fi

# 打包模块
echo "[3/4] 打包模块..."
mkdir -p "$OUTDIR"
cd "$MODDIR"
zip -r "../${OUTDIR}/bandai-${VERSION}.zip" \
    module.prop \
    post-fs-data.sh \
    service.sh \
    customize.sh \
    uninstall.sh \
    config.template.json \
    bin/bandai \
    ca/gen_ca.py \
    -x "*.DS_Store"
cd ..

echo "[4/4] 完成!"
echo ""
echo "输出文件: ${OUTDIR}/bandai-${VERSION}.zip"
ls -lh "${OUTDIR}/bandai-${VERSION}.zip"
echo ""
echo "刷入方法: Magisk / KernelSU → 从本地安装 → 选择 zip → 重启"
