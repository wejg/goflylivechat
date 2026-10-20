package common

import (
	"encoding/json"
	"goflylivechat/tools"
	"io/ioutil"
	"os"
	"strings"
)

type Mysql struct {
	Server   string
	Port     string
	Database string
	Username string
	Password string
}

func GetMysqlConf() *Mysql {
	var mysql = &Mysql{}
	isExist, _ := tools.IsFileExist(MysqlConf)
	if isExist {
		info, err := ioutil.ReadFile(MysqlConf)
		if err == nil {
			_ = json.Unmarshal(info, mysql)
		}
	}
	mysql.Server = envOr("GOFLY_DB_SERVER", mysql.Server)
	mysql.Port = envOr("GOFLY_DB_PORT", mysql.Port)
	mysql.Database = envOr("GOFLY_DB_DATABASE", mysql.Database)
	mysql.Username = envOr("GOFLY_DB_USERNAME", mysql.Username)
	mysql.Password = envOr("GOFLY_DB_PASSWORD", mysql.Password)
	return mysql
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func IsAllowedOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	configured := strings.TrimSpace(os.Getenv("GOFLY_ALLOWED_ORIGINS"))
	if configured == "" {
		return true
	}
	for _, allowed := range strings.Split(configured, ",") {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

func InstallLockPath() string {
	if value := os.Getenv("GOFLY_INSTALL_LOCK"); value != "" {
		return value
	}
	return "./install.lock"
}
