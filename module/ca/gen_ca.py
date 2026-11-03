#!/usr/bin/env python3
"""
生成 BandAI 专用的 CA 证书。
运行后在 ca/ 目录下生成 ca.crt、ca.key 和 Android hash 命名的证书文件。

用法: python3 gen_ca.py
"""

import hashlib
import struct
import os
from datetime import datetime, timedelta

try:
    from cryptography import x509
    from cryptography.x509.oid import NameOID
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import rsa
except ImportError:
    print("需要安装 cryptography: pip install cryptography")
    exit(1)


def android_hash(cert):
    """计算 Android system cacerts 文件名（MD5 of subject DER, old format）"""
    h = hashlib.md5(cert.subject.public_bytes()).digest()
    return format(struct.unpack("<I", h[:4])[0], "08x") + ".0"


def main():
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)

    subject = issuer = x509.Name([
        x509.NameAttribute(NameOID.COMMON_NAME, "BandAI Root CA"),
        x509.NameAttribute(NameOID.ORGANIZATION_NAME, "BandAI"),
    ])

    cert = (
        x509.CertificateBuilder()
        .subject_name(subject)
        .issuer_name(issuer)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(datetime.utcnow())
        .not_valid_after(datetime.utcnow() + timedelta(days=3650))
        .add_extension(x509.BasicConstraints(ca=True, path_length=1), critical=True)
        .sign(key, hashes.SHA256())
    )

    os.makedirs("ca", exist_ok=True)

    # 私钥
    with open("ca/ca.key", "wb") as f:
        f.write(key.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.TraditionalOpenSSL,
            serialization.NoEncryption(),
        ))

    # 证书 PEM
    cert_pem = cert.public_bytes(serialization.Encoding.PEM)
    with open("ca/ca.crt", "wb") as f:
        f.write(cert_pem)

    # Android hash 命名
    fname = android_hash(cert)
    with open(f"ca/{fname}", "wb") as f:
        f.write(cert_pem)

    print(f"CA 生成完成:")
    print(f"  ca/ca.crt  - 证书")
    print(f"  ca/ca.key  - 私钥")
    print(f"  ca/{fname} - Android cacerts 文件")
    print(f"\n将 ca/ 目录复制到模块中即可。")


if __name__ == "__main__":
    main()
