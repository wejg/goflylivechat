package controller

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"goflylivechat/common"
	"goflylivechat/models"
	"goflylivechat/tools"
	"goflylivechat/ws"
	"log"
	"os"
	"strings"
)

func PostInstall(c *gin.Context) {
	lockPath := common.InstallLockPath()
	notExist, _ := tools.IsFileNotExist(lockPath)
	if !notExist {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "系统已经安装过了",
		})
		return
	}
	server := c.PostForm("server")
	port := c.PostForm("port")
	database := c.PostForm("database")
	username := c.PostForm("username")
	password := c.PostForm("password")
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", username, password, server, port, database)
	testDB, err := gorm.Open("mysql", dsn)
	if err != nil {
		log.Println(err)
		tools.Logger().Println(err)
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "数据库连接失败:" + err.Error(),
		})
		return
	}
	_ = testDB.Close()
	isExist, _ := tools.IsFileExist(common.Dir)
	if !isExist {
		os.Mkdir(common.Dir, os.ModePerm)
	}
	fileConfig := common.MysqlConf
	file, err := os.OpenFile(fileConfig, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": "数据库配置保存失败"})
		return
	}
	defer file.Close()

	format := `{
	"Server":"%s",
	"Port":"%s",
	"Database":"%s",
	"Username":"%s",
	"Password":"%s"
}
`
	data := fmt.Sprintf(format, server, port, database, username, password)
	file.WriteString(data)
	models.Connect()
	installFile, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": "安装锁保存失败"})
		return
	}
	defer installFile.Close()
	_, _ = installFile.WriteString("gofly live chat")
	ok, err := install()
	if !ok {
		c.JSON(200, gin.H{
			"code": 200,
			"msg":  err.Error(),
		})
		return
	}
	models.LoadConfigs()
	c.JSON(200, gin.H{
		"code": 200,
		"msg":  "安装成功",
	})
}
func install() (bool, error) {
	sqlFile := "import.sql"
	if exists, _ := tools.IsFileExist(sqlFile); !exists {
		return false, errors.New("import.sql 不存在")
	}
	sqls, err := os.ReadFile(sqlFile)
	if err != nil {
		return false, err
	}
	for _, sql := range strings.Split(string(sqls), ";") {
		sql = strings.TrimSpace(sql)
		if sql == "" {
			continue
		}
		if err := models.Execute(sql); err != nil {
			return false, err
		}
	}
	return true, nil
}

func GetStatistics(c *gin.Context) {
	visitors := models.CountVisitors()
	message := models.CountMessage(nil, nil)
	session := ws.ClientList.Len()
	kefuNum := 0
	c.JSON(200, gin.H{
		"code": 200,
		"msg":  "ok",
		"result": gin.H{
			"visitors": visitors,
			"message":  message,
			"session":  session + kefuNum,
		},
	})
}
