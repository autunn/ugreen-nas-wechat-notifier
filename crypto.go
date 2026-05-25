package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
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