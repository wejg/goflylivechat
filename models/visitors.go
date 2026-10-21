package models

import (
	"time"
)

type Visitor struct {
	Model
	Name        string `json:"name"`
	Avator      string `json:"avator"`
	SourceIp    string `json:"source_ip"`
	ToId        string `json:"to_id"`
	VisitorId   string `json:"visitor_id"`
	Status      uint   `json:"status"`
	Refer       string `json:"refer"`
	City        string `json:"city"`
	ClientIp    string `json:"client_ip"`
	LastMessage string `json:"last_message"`
	Extra       string `json:"extra"`
}

func CreateVisitor(name, avator, sourceIp, toId, visitorId, refer, city, clientIp, extra string) (Visitor, error) {
	v := &Visitor{
		Name:      name,
		Avator:    avator,
		SourceIp:  sourceIp,
		ToId:      toId,
		VisitorId: visitorId,
		Status:    1,
		Refer:     refer,
		City:      city,
		ClientIp:  clientIp,
		Extra:     extra,
	}
	v.UpdatedAt = time.Now()
	err := DB.Create(v).Error
	return *v, err
}
func FindVisitorByVistorId(visitorId string) Visitor {
	var v Visitor
	DB.Where("visitor_id = ?", visitorId).First(&v)
	return v
}
func FindVisitorsByKefuId(page uint, pagesize uint, kefuId string) []Visitor {
	if page == 0 {
		page = 1
	}
	if pagesize == 0 {
		pagesize = 10
	}
	offset := (page - 1) * pagesize
	if offset <= 0 {
		offset = 0
	}
	var visitors []Visitor
	//sql := fmt.Sprintf("select * from visitor where id>=(select id from visitor where  to_id='%s' order by updated_at desc limit %d,1) and to_id='%s' order by updated_at desc limit %d ", kefuId, offset, kefuId, pagesize)
	//DB.Raw(sql).Scan(&visitors)
	DB.Where("to_id=?", kefuId).Offset(offset).Limit(pagesize).Order("updated_at desc").Find(&visitors)
	return visitors
}

// FindOnlineVisitorIds 只取 visitor_id，避免把整行读进内存。
func FindOnlineVisitorIds() []string {
	var ids []string
	DB.Model(&Visitor{}).Where("status = ?", 1).Pluck("visitor_id", &ids)
	return ids
}
func UpdateVisitorLastMessage(visitorId, lastMessage string) {
	visitor := Visitor{}
	DB.Model(&visitor).Where("visitor_id = ?", visitorId).Update("last_message", lastMessage)
}
func UpdateVisitorStatus(visitorId string, status uint) {
	visitor := Visitor{}
	DB.Model(&visitor).Where("visitor_id = ?", visitorId).Update("status", status)
}

// UpdateVisitorsOffline 用一条 UPDATE 批量下线，代替每个访客一条语句一个事务。
func UpdateVisitorsOffline(visitorIds []string) {
	if len(visitorIds) == 0 {
		return
	}
	DB.Model(&Visitor{}).Where("visitor_id IN (?)", visitorIds).Update("status", 0)
}
func UpdateVisitor(name, avator, visitorId string, status uint, clientIp string, sourceIp string, refer, extra string) error {
	visitor := &Visitor{
		Status:   status,
		ClientIp: clientIp,
		SourceIp: sourceIp,
		Refer:    refer,
		Extra:    extra,
		Name:     name,
		Avator:   avator,
	}
	visitor.UpdatedAt = time.Now()
	return DB.Model(visitor).Where("visitor_id = ?", visitorId).Update(visitor).Error
}
func UpdateVisitorKefu(visitorId, fromId, kefuId string) (bool, error) {
	result := DB.Model(&Visitor{}).Where("visitor_id = ? AND to_id = ?", visitorId, fromId).Update("to_id", kefuId)
	return result.RowsAffected == 1, result.Error
}

// 查询条数
func CountVisitors() uint {
	var count uint
	DB.Model(&Visitor{}).Count(&count)
	return count
}

// 查询条数
func CountVisitorsByKefuId(kefuId string) uint {
	var count uint
	DB.Model(&Visitor{}).Where("to_id=?", kefuId).Count(&count)
	return count
}

// 查询每天条数
type EveryDayNum struct {
	Day string `json:"day"`
	Num int64  `json:"num"`
}

func CountVisitorsEveryDay(toId string) []EveryDayNum {
	var results []EveryDayNum
	DB.Raw("select DATE_FORMAT(created_at,'%y-%m-%d') as day ,"+
		"count(*) as num from visitor where to_id=? group by day order by day desc limit 30",
		toId).Scan(&results)
	return results
}
