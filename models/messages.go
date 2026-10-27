package models

import (
	"fmt"
	"time"

	"github.com/jinzhu/gorm"
)

type Message struct {
	Model
	KefuId          string  `json:"kefu_id"`
	VisitorId       string  `json:"visitor_id"`
	Content         string  `json:"content"`
	MesType         string  `json:"mes_type"`
	Status          string  `json:"status"`
	ClientMessageID *string `json:"-" gorm:"column:client_message_id"`
	AutoReplyType   string  `json:"-"`
}
type MessageKefu struct {
	Model
	KefuId        string `json:"kefu_id"`
	VisitorId     string `json:"visitor_id"`
	Content       string `json:"content"`
	MesType       string `json:"mes_type"`
	Status        string `json:"status"`
	VisitorName   string `json:"visitor_name"`
	VisitorAvator string `json:"visitor_avator"`
	KefuName      string `json:"kefu_name"`
	KefuAvator    string `json:"kefu_avator"`
	CreateTime    string `json:"create_time"`
}

func CreateMessage(kefuID string, visitorID string, content string, mesType string, clientMessageID ...string) (uint, error) {
	return createMessage(kefuID, visitorID, content, mesType, "", 0, clientMessageID...)
}

func CreateAutoReply(kefuID, visitorID, content, replyType string, visitorMessageID uint) (uint, error) {
	if replyType != "offline" && replyType != "keyword" {
		return 0, fmt.Errorf("invalid auto reply type")
	}
	return createMessage(kefuID, visitorID, content, "kefu", replyType, visitorMessageID)
}

func createMessage(kefuID, visitorID, content, mesType, autoReplyType string, visitorMessageID uint, clientMessageID ...string) (uint, error) {
	db := DB
	if mesType == "kefu" {
		db = DB.Begin()
		if db.Error != nil {
			return 0, db.Error
		}
		defer db.Rollback()
		var visitor Visitor
		if err := db.Set("gorm:query_option", "FOR UPDATE").Where("visitor_id=?", visitorID).First(&visitor).Error; err != nil {
			return 0, err
		}
		if autoReplyType != "" && visitor.ToId != kefuID {
			return 0, nil
		}
		if autoReplyType == "offline" {
			var lastReply Message
			err := db.Where("visitor_id=? AND kefu_id=? AND mes_type=? AND auto_reply_type<>?", visitorID, kefuID, "kefu", "keyword").Order("id DESC").First(&lastReply).Error
			if err != nil && err != gorm.ErrRecordNotFound {
				return 0, err
			}
			if err == nil && (lastReply.AutoReplyType == "offline" || lastReply.ID > visitorMessageID) {
				return 0, nil
			}
		}
	}
	var key *string
	if len(clientMessageID) > 0 && clientMessageID[0] != "" {
		value := clientMessageID[0]
		key = &value
		var existing Message
		err := db.Where("client_message_id=?", value).First(&existing).Error
		if err == nil {
			if existing.KefuId != kefuID || existing.VisitorId != visitorID || existing.MesType != mesType {
				return 0, fmt.Errorf("client message id belongs to another conversation")
			}
			return existing.ID, nil
		}
		if err != gorm.ErrRecordNotFound {
			return 0, err
		}
	}
	v := &Message{KefuId: kefuID, VisitorId: visitorID, Content: content, MesType: mesType, Status: "unread", ClientMessageID: key, AutoReplyType: autoReplyType}
	v.UpdatedAt = time.Now()
	if err := db.Create(v).Error; err != nil {
		if key != nil {
			var existing Message
			if lookupErr := db.Where("client_message_id=?", *key).First(&existing).Error; lookupErr == nil {
				if existing.KefuId != kefuID || existing.VisitorId != visitorID || existing.MesType != mesType {
					return 0, fmt.Errorf("client message id belongs to another conversation")
				}
				return existing.ID, nil
			}
		}
		return 0, err
	}
	if mesType == "kefu" {
		if err := db.Commit().Error; err != nil {
			return 0, err
		}
	}
	return v.ID, nil
}
func FindMessageByClientID(clientMessageID string) (Message, bool, error) {
	if clientMessageID == "" {
		return Message{}, false, nil
	}
	var message Message
	err := DB.Where("client_message_id=?", clientMessageID).First(&message).Error
	if err == gorm.ErrRecordNotFound {
		return Message{}, false, nil
	}
	return message, err == nil, err
}

func FindMessageByVisitorId(visitor_id string) []Message {
	var messages []Message
	DB.Where("visitor_id=?", visitor_id).Order("id asc").Find(&messages)
	return messages
}

// ReadMessageByVisitorId 只确认到已展示的消息ID，保留更晚到达的消息。
func ReadMessageByVisitorId(visitorID, mesType string, lastMessageID uint) error {
	return DB.Model(&Message{}).
		Where("visitor_id=? AND mes_type=? AND status=? AND id<=?", visitorID, mesType, "unread", lastMessageID).
		Update("status", "read").Error
}

func FindUnreadMessageNumByVisitorId(visitorID, mesType string) (uint, error) {
	var count uint
	err := DB.Model(&Message{}).Where("visitor_id=? AND mes_type=? AND status=?", visitorID, mesType, "unread").Count(&count).Error
	return count, err
}

type VisitorUnread struct {
	VisitorId     string
	Unread        uint
	LastMessageID uint
}

func FindKefuUnreadMessages(kefuName string) ([]VisitorUnread, error) {
	var counts []VisitorUnread
	err := DB.Table("message").
		Select("message.visitor_id, COUNT(*) AS unread, MAX(message.id) AS last_message_id").
		Joins("JOIN visitor ON visitor.visitor_id = message.visitor_id").
		Where("visitor.to_id=? AND visitor.deleted_at IS NULL AND message.deleted_at IS NULL AND message.mes_type=? AND message.status=?", kefuName, "visitor", "unread").
		Group("message.visitor_id").Scan(&counts).Error
	return counts, err
}

func ReadKefuMessages(kefuName string, lastMessageID uint) error {
	visitors := DB.Table("visitor").Select("visitor_id").Where("to_id=? AND deleted_at IS NULL", kefuName).SubQuery()
	return DB.Model(&Message{}).
		Where("visitor_id IN (?) AND mes_type=? AND status=? AND id<=?", visitors, "visitor", "unread", lastMessageID).
		Update("status", "read").Error
}

// 查询最后一条消息
func FindLastMessage(visitorIds []string) []Message {
	var messages []Message
	if len(visitorIds) <= 0 {
		return messages
	}
	var ids []Message
	DB.Select("MAX(id) id").Where(" visitor_id in (? )", visitorIds).Group("visitor_id").Find(&ids)
	if len(ids) <= 0 {
		return messages
	}
	var idStr = make([]string, 0, 0)
	for _, mes := range ids {
		idStr = append(idStr, fmt.Sprintf("%d", mes.ID))
	}
	DB.Select("visitor_id,id,content").Where(" id in (? )", idStr).Find(&messages)
	//subQuery := DB.
	//	Table("message").
	//	Where(" visitor_id in (? )", visitorIds).
	//	Order("id desc").
	//	Limit(1024).
	//	SubQuery()
	//DB.Raw("SELECT ANY_VALUE(visitor_id) visitor_id,ANY_VALUE(id) id,ANY_VALUE(content) content FROM ? message_alia GROUP BY visitor_id", subQuery).Scan(&messages)
	//DB.Select("ANY_VALUE(visitor_id) visitor_id,MAX(ANY_VALUE(id)) id,ANY_VALUE(content) content").Group("visitor_id").Find(&messages)
	return messages
}

// 查询最后一条消息
func FindLastMessageByVisitorId(visitorId string) Message {
	var m Message
	DB.Select("content").Where("visitor_id=?", visitorId).Order("id desc").First(&m)
	return m
}
func FindMessageByWhere(query interface{}, args ...interface{}) []MessageKefu {
	var messages []MessageKefu
	DB.Table("message").Where(query, args...).Select("message.*,visitor.avator visitor_avator,visitor.name visitor_name,user.avator kefu_avator,user.nickname kefu_name").Joins("left join user on message.kefu_id=user.name").Joins("left join visitor on visitor.visitor_id=message.visitor_id").Order("message.id asc").Find(&messages)
	return messages
}

// 查询条数
func CountMessage(query interface{}, args ...interface{}) uint {
	var count uint
	DB.Model(&Message{}).Where(query, args...).Count(&count)
	return count
}

// 分页查询
func FindMessageByPage(page uint, pagesize uint, newer bool, query interface{}, args ...interface{}) []*MessageKefu {
	if page == 0 {
		page = 1
	}
	if pagesize == 0 {
		pagesize = 10
	}
	offset := (page - 1) * pagesize
	if offset < 0 {
		offset = 0
	}
	order := "message.id desc"
	if newer {
		order = "message.id asc"
	}
	var messages []*MessageKefu
	DB.Table("message").Select("message.*,visitor.avator visitor_avator,visitor.name visitor_name,user.avator kefu_avator,user.nickname kefu_name").Offset(offset).Joins("left join user on message.kefu_id=user.name").Joins("left join visitor on visitor.visitor_id=message.visitor_id").Where(query, args...).Limit(pagesize).Order(order).Find(&messages)
	for _, mes := range messages {
		mes.CreateTime = mes.CreatedAt.UTC().Format(time.RFC3339)
	}
	return messages
}
