package tools

import (
	"golang.org/x/crypto/bcrypt"
	"strings"
)

// HashPassword 生成 bcrypt 摘要，用于新建和修改密码。
func HashPassword(plain string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

// CheckPassword 校验明文密码是否匹配已存储的摘要。
//
// 历史数据是 32 位无盐 MD5，这里同时兼容两种格式：
// 返回的第二个值表示「这条记录还是旧的 MD5，应当重写成 bcrypt」，
// 调用方据此在登录成功后无感升级。
func CheckPassword(stored, plain string) (ok bool, needsUpgrade bool) {
	if stored == "" {
		return false, false
	}
	if isBcryptHash(stored) {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(plain)) == nil, false
	}
	// 旧的 MD5 记录。
	if Md5(plain) == stored {
		return true, true
	}
	return false, false
}

func isBcryptHash(value string) bool {
	return strings.HasPrefix(value, "$2a$") ||
		strings.HasPrefix(value, "$2b$") ||
		strings.HasPrefix(value, "$2y$")
}
