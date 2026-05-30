package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
)

// RsaEncrypt uses RSA PKCS#1 v1.5 only because the UGREEN local API expects it.
// New protocols should prefer RSA-OAEP as recommended by Go's crypto/rsa docs.
func RsaEncrypt(pubKeyPem string, plaintext string) (string, error) {
	block, _ := pem.Decode([]byte(pubKeyPem))
	if block == nil {
		return "", errors.New("无法解析 PEM 公钥")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("不是有效的 RSA 公钥")
	}
	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, []byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// AESGCMEncrypt AES-256-GCM 加密
func AESGCMEncrypt(keyHex string, plaintext string) (string, error) {
	key := []byte(keyHex) // 32 bytes
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	ct := aesgcm.Seal(nil, iv, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(append(iv, ct...)), nil
}

// AESGCMDecrypt AES-256-GCM 解密
func AESGCMDecrypt(keyHex string, encoded string) (string, error) {
	key := []byte(keyHex)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	if len(raw) < 12 {
		return "", errors.New("密文太短")
	}
	iv := raw[:12]
	ct := raw[12:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := aesgcm.Open(nil, iv, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// MD5Hex 计算字符串的 MD5 并返回小写 Hex
func MD5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
