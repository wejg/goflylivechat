package models

import (
	"sync"

	"github.com/jinzhu/gorm"
)

var (
	CustomConfigs []Config
	configMu      sync.RWMutex
)

func LoadConfigs() {
	if DB != nil {
		configs := FindConfigs()
		configMu.Lock()
		CustomConfigs = configs
		configMu.Unlock()
	}
}

type Config struct {
	ID        uint   `gorm:"primary_key" json:"id"`
	ConfName  string `json:"conf_name"`
	ConfKey   string `json:"conf_key"`
	ConfValue string `json:"conf_value"`
	UserId    string `json:"user_id"`
}

func UpdateConfig(userid interface{}, key string, value string) error {
	var config Config
	err := DB.Where("user_id = ? and conf_key = ?", userid, key).First(&config).Error
	if err != nil && !gorm.IsRecordNotFoundError(err) {
		return err
	}
	if config.ID != 0 {
		if err := DB.Model(&Config{}).Where("user_id = ? and conf_key = ?", userid, key).Update("conf_value", value).Error; err != nil {
			return err
		}
		config.ConfValue = value
	} else {
		config = Config{ConfKey: key, ConfValue: value, UserId: userid.(string)}
		if err := DB.Create(&config).Error; err != nil {
			return err
		}
	}
	configMu.Lock()
	defer configMu.Unlock()
	for i := range CustomConfigs {
		if CustomConfigs[i].UserId == config.UserId && CustomConfigs[i].ConfKey == config.ConfKey {
			CustomConfigs[i] = config
			return nil
		}
	}
	CustomConfigs = append(CustomConfigs, config)
	return nil
}
func FindConfigs() []Config {
	var config []Config
	DB.Find(&config)
	return config
}
func FindConfigsByUserId(userid interface{}) []Config {
	var config []Config
	DB.Where("user_id = ?", userid).Find(&config)
	return config
}

// FindConfig 按 key 查全局配置，不区分 user_id。
// 只适用于确实全局唯一的配置；凡是按客服区分的（邮件、推送），
// 必须用 FindUserConfig，否则多客服时会拿到别人的配置。
func FindConfig(key string) string {
	configMu.RLock()
	defer configMu.RUnlock()
	for _, config := range CustomConfigs {
		if key == config.ConfKey {
			return config.ConfValue
		}
	}
	return ""
}

// FindUserConfig 返回指定客服的配置值，缓存未命中时回落到全局值。
func FindUserConfig(userId string, key string) string {
	configMu.RLock()
	defer configMu.RUnlock()
	for _, config := range CustomConfigs {
		if config.ConfKey == key && config.UserId == userId {
			return config.ConfValue
		}
	}
	return ""
}
func FindConfigByUserId(userId interface{}, key string) Config {
	var config Config
	DB.Where("user_id = ? and conf_key = ?", userId, key).Find(&config)
	return config
}
