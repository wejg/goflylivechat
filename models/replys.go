package models

import "github.com/jinzhu/gorm"

type ReplyItem struct {
	Id       string `json:"item_id"`
	Content  string `json:"item_content"`
	GroupId  string `json:"group_id"`
	ItemName string `json:"item_name"`
	UserId   string `json:"user_id"`
}
type ReplyGroup struct {
	Id        string       `json:"group_id"`
	GroupName string       `json:"group_name"`
	UserId    string       `json:"user_id"`
	Items     []*ReplyItem `json:"items"`
}

func FindReplyItemByUserIdTitle(userId interface{}, title string) ReplyItem {
	var reply ReplyItem
	DB.Where("user_id = ? and item_name = ?", userId, title).Find(&reply)
	return reply
}
func FindReplyByUserId(userId interface{}) []*ReplyGroup {
	var replyGroups []*ReplyGroup
	//DB.Raw("select a.*,b.* from reply_group a left join reply_item b on a.id=b.group_id where a.user_id=? ", userId).Scan(&replyGroups)
	var replyItems []*ReplyItem
	DB.Where("user_id = ?", userId).Find(&replyGroups)
	DB.Where("user_id = ?", userId).Find(&replyItems)
	temp := make(map[string]*ReplyGroup)
	for _, replyGroup := range replyGroups {
		replyGroup.Items = make([]*ReplyItem, 0)
		temp[replyGroup.Id] = replyGroup
	}
	for _, replyItem := range replyItems {
		if group := temp[replyItem.GroupId]; group != nil {
			group.Items = append(group.Items, replyItem)
		}
	}
	return replyGroups
}
func FindReplyTitleByUserId(userId interface{}) []*ReplyGroup {
	var replyGroups []*ReplyGroup
	//DB.Raw("select a.*,b.* from reply_group a left join reply_item b on a.id=b.group_id where a.user_id=? ", userId).Scan(&replyGroups)
	var replyItems []*ReplyItem
	DB.Where("user_id = ?", userId).Find(&replyGroups)
	DB.Select("item_name,group_id").Where("user_id = ?", userId).Find(&replyItems)
	temp := make(map[string]*ReplyGroup)
	for _, replyGroup := range replyGroups {
		replyGroup.Items = make([]*ReplyItem, 0)
		temp[replyGroup.Id] = replyGroup
	}
	for _, replyItem := range replyItems {
		if group := temp[replyItem.GroupId]; group != nil {
			group.Items = append(group.Items, replyItem)
		}
	}
	return replyGroups
}
func CreateReplyGroup(groupName string, userId string) error {
	return DB.Create(&ReplyGroup{GroupName: groupName, UserId: userId}).Error
}
func CreateReplyContent(groupId string, userId string, content, itemName string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var group ReplyGroup
		// 创建和删除锁定同一父行，分组删除后不能再插入回复。
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ? and id = ?", userId, groupId).First(&group).Error; err != nil {
			return err
		}
		return tx.Create(&ReplyItem{GroupId: groupId, UserId: userId, Content: content, ItemName: itemName}).Error
	})
}
func UpdateReplyContent(id, userId, title, content string) error {
	result := DB.Model(&ReplyItem{}).Where("user_id = ? and id = ?", userId, id).Updates(map[string]interface{}{"item_name": title, "content": content})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return DB.Where("user_id = ? and id = ?", userId, id).First(&ReplyItem{}).Error
	}
	return nil
}
func DeleteReplyContent(id string, userId string) error {
	result := DB.Where("user_id = ? and id = ?", userId, id).Delete(ReplyItem{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
func DeleteReplyGroup(id string, userId string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var group ReplyGroup
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ? and id = ?", userId, id).First(&group).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? and group_id = ?", userId, id).Delete(ReplyItem{}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ? and id = ?", userId, id).Delete(ReplyGroup{}).Error
	})
}
func FindReplyBySearcch(userId interface{}, search string) []*ReplyGroup {
	var replyGroups []*ReplyGroup
	var replyItems []*ReplyItem
	DB.Where("user_id = ?", userId).Find(&replyGroups)
	DB.Where("user_id = ? and content like ?", userId, "%"+search+"%").Find(&replyItems)
	temp := make(map[string]*ReplyGroup)
	for _, replyGroup := range replyGroups {
		replyGroup.Items = make([]*ReplyItem, 0)
		temp[replyGroup.Id] = replyGroup
	}
	for _, replyItem := range replyItems {
		if group := temp[replyItem.GroupId]; group != nil {
			group.Items = append(group.Items, replyItem)
		}
	}
	var newReplyGroups []*ReplyGroup = make([]*ReplyGroup, 0)
	for _, replyGroup := range replyGroups {
		if len(replyGroup.Items) != 0 {
			newReplyGroups = append(newReplyGroups, replyGroup)
		}
	}
	return newReplyGroups
}
