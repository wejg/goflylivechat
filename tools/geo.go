package tools

import (
	"github.com/oschwald/geoip2-golang"
	"log"
	"net"
	"sync"
)

var (
	geoOnce   sync.Once
	geoReader *geoip2.Reader
)

// geoip2.Reader 并发安全，进程内打开一次即可。
// 这个库有 58MB，每次访客登录都 open/mmap/close 是实打实的浪费。
func openGeoDB(path string) *geoip2.Reader {
	geoOnce.Do(func() {
		db, err := geoip2.Open(path)
		if err != nil {
			log.Println("geoip open:", err)
			return
		}
		geoReader = db
	})
	return geoReader
}

// GetCity 查询 IP 归属地，失败时返回空字符串，调用方据此降级。
func GetCity(path, ipAddress string) (string, string) {
	db := openGeoDB(path)
	if db == nil {
		return "", ""
	}
	ip := net.ParseIP(ipAddress)
	if ip == nil {
		return "", ""
	}
	record, err := db.City(ip)
	if err != nil || record == nil {
		return "", ""
	}
	return record.Country.Names["en"], record.City.Names["en"]
}
