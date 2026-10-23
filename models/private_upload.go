package models

import (
	"errors"
	"github.com/jinzhu/gorm"
	"goflylivechat/tools"
	"time"
)

type PrivateUpload struct {
	ID        string `gorm:"type:varchar(36);primary_key"`
	Path      string `gorm:"type:varchar(512);not null"`
	Name      string `gorm:"type:varchar(255);not null"`
	VisitorID string `gorm:"type:varchar(100);not null;index"`
	AgentID   string `gorm:"type:varchar(100);not null"`
	CreatedAt time.Time
}

func (PrivateUpload) TableName() string { return "private_upload" }
func EnsureUploadSchema() error {
	if DB == nil {
		return errors.New("database unavailable")
	}
	return DB.AutoMigrate(&PrivateUpload{}).Error
}
func RecordPrivateUpload(filePath, name, visitorID, agentID string) (PrivateUpload, error) {
	row := PrivateUpload{ID: tools.Uuid(), Path: filePath, Name: name, VisitorID: visitorID, AgentID: agentID}
	if DB == nil || visitorID == "" && agentID == "" {
		return row, errors.New("upload owner required")
	}
	return row, DB.Create(&row).Error
}
func CanReadPrivateUpload(id, visitorID, agentID string) (bool, error) {
	if DB == nil {
		return false, errors.New("database unavailable")
	}
	var upload PrivateUpload
	if err := DB.Where("id=?", id).First(&upload).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			return false, nil
		}
		return false, err
	}
	if visitorID != "" {
		return upload.VisitorID == visitorID, nil
	}
	if agentID == "" {
		return false, nil
	}
	if upload.VisitorID == "" {
		return upload.AgentID == agentID, nil
	}
	var visitor Visitor
	err := DB.Where("visitor_id=? AND to_id=?", upload.VisitorID, agentID).First(&visitor).Error
	if gorm.IsRecordNotFoundError(err) {
		return false, nil
	}
	return err == nil, err
}
