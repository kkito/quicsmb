// Package auth 做预共享 Key 的包认证：HMAC 校验，失败无差别拒绝（黑洞）。
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
)

const nonceLen = 16
const macLen = 32

func Nonce16() ([16]byte, error) {
	var n [16]byte
	_, err := rand.Read(n[:])
	return n, err
}

func Seal(key, nonce []byte, payload []byte) []byte {
	if len(nonce) != nonceLen {
		var n [nonceLen]byte
		copy(n[:], nonce)
		nonce = n[:]
	}
	m := hmac.New(sha256.New, key)
	m.Write(nonce)
	m.Write(payload)
	out := make([]byte, 0, nonceLen+macLen+len(payload))
	out = append(out, nonce...)
	out = append(out, m.Sum(nil)...)
	out = append(out, payload...)
	return out
}

func Open(key []byte, msg []byte) ([]byte, bool) {
	if len(msg) < nonceLen+macLen {
		return nil, false
	}
	nonce := msg[:nonceLen]
	want := msg[nonceLen : nonceLen+macLen]
	payload := msg[nonceLen+macLen:]
	m := hmac.New(sha256.New, key)
	m.Write(nonce)
	m.Write(payload)
	if !hmac.Equal(m.Sum(nil), want) {
		return nil, false
	}
	return payload, true
}
