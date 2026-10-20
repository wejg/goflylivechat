package models

import (
	"errors"
	"fmt"
	"github.com/jinzhu/gorm"
	"goflylivechat/common"
	"log"
	"os"
	"strconv"
	"time"
)

var DB *gorm.DB

type Model struct {
	ID        uint       `gorm:"primary_key" json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `sql:"index" json:"deleted_at"`
}

func init() {
	// 连不上数据库不在 import 阶段 panic：
	// 那样会让任何引用本包的测试都无法运行，也会让启动期的短暂抖动直接打死进程。
	// 真正需要数据库的命令在启动时调用 MustConnect。
	if err := Connect(); err != nil {
		log.Println("database connect:", err)
	}
}

// MustConnect 供服务启动路径使用：连不上就退出，不要带着空 DB 继续跑。
func MustConnect() {
	if err := Connect(); err != nil {
		log.Fatalln("数据库连接失败:", err)
	}
}

func Connect() error {
	mysql := common.GetMysqlConf()
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", mysql.Username, mysql.Password, mysql.Server, mysql.Port, mysql.Database)
	if DB != nil {
		_ = DB.Close()
		DB = nil
	}
	db, err := gorm.Open("mysql", dsn)
	if err != nil {
		return err
	}
	db.SingularTable(true)
	debugSQL, _ := strconv.ParseBool(os.Getenv("GOFLY_DB_LOG"))
	db.LogMode(debugSQL)
	db.DB().SetMaxIdleConns(10)
	db.DB().SetMaxOpenConns(100)
	db.DB().SetConnMaxLifetime(59 * time.Second)
	DB = db
	Migrate()
	LoadConfigs()
	return nil
}
func Execute(sql string) error {
	if DB == nil {
		return errors.New("数据库未连接")
	}
	return DB.Exec(sql).Error
}
func CloseDB() {
	if DB == nil {
		return
	}
	_ = DB.Close()
}
