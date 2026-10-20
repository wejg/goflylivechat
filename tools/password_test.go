package tools

import "testing"

func TestCheckPasswordBcrypt(t *testing.T) {
	hashed, err := HashPassword("s3cret")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	ok, upgrade := CheckPassword(hashed, "s3cret")
	if !ok {
		t.Fatal("正确密码应当通过")
	}
	if upgrade {
		t.Fatal("bcrypt 记录不需要升级")
	}
	if ok, _ := CheckPassword(hashed, "wrong"); ok {
		t.Fatal("错误密码不应通过")
	}
}

// 旧库里是无盐 MD5，登录必须仍然可用，并且标记为需要升级。
func TestCheckPasswordLegacyMd5(t *testing.T) {
	legacy := Md5("s3cret")
	ok, upgrade := CheckPassword(legacy, "s3cret")
	if !ok {
		t.Fatal("旧 MD5 密码应当仍可登录")
	}
	if !upgrade {
		t.Fatal("旧 MD5 记录应当标记为需要升级")
	}
	if ok, _ := CheckPassword(legacy, "wrong"); ok {
		t.Fatal("错误密码不应通过")
	}
}

func TestCheckPasswordEmpty(t *testing.T) {
	if ok, _ := CheckPassword("", "anything"); ok {
		t.Fatal("空摘要不应通过")
	}
}

// bcrypt 摘要 60 字符，user.password 必须至少 varchar(100)，
// 否则会被静默截断导致所有登录失败。
func TestBcryptHashFitsColumn(t *testing.T) {
	hashed, err := HashPassword("whatever")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if len(hashed) > 100 {
		t.Fatalf("摘要长度 %d 超过 user.password 列宽", len(hashed))
	}
}
