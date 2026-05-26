package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
)

// RsaEncrypt 使用公钥进行 RSA PKCS1v1.5 加密，返回 Base64 字符串
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

// ==================== 以下为飞牛 (fnos) 所需的算法 ====================

// AesEncrypt AES-CBC 加密
func AesEncrypt(data string, key string, iv []byte) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	paddedData := pkcs7Pad([]byte(data), aes.BlockSize)
	ciphertext := make([]byte, len(paddedData))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, paddedData)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// AesDecrypt AES-CBC 解密
func AesDecrypt(cipherBase64 string, key string, iv []byte) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(cipherBase64)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return "", errors.New("密文长度不是块大小的整数倍")
	}
	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)

	unpaddedData := pkcs7Unpad(plaintext)
	return base64.StdEncoding.EncodeToString(unpaddedData), nil
}

// GetSignature HMAC-SHA256 签名
func GetSignature(data string, keyBase64 string) (string, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, keyBytes)
	mac.Write([]byte(data))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// pkcs7Pad 补码
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padText...)
}

// pkcs7Unpad 去码
func pkcs7Unpad(data []byte) []byte {
	length := len(data)
	if length == 0 {
		return data
	}
	unpadding := int(data[length-1])
	return data[:(length - unpadding)]
}

// ==================== 以下为绿联 (ugreen) 深层 API 所需的算法 ====================

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
	rand.Read(iv)
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
