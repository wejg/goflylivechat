package controller

import (
	"errors"
	"goflylivechat/models"
	"goflylivechat/tools"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Saving an agent avatar explicitly publishes this image; conversation uploads stay private.
func publishAgentAvatar(value, agent string) (string, error) {
	if !strings.HasPrefix(value, "/attachments/") {
		return value, nil
	}
	var row models.PrivateUpload
	if err := models.DB.Where("id=? AND agent_id=? AND visitor_id=''", strings.TrimPrefix(value, "/attachments/"), agent).First(&row).Error; err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(row.Name))
	if !allowedImageExts[ext] {
		return "", errors.New("头像必须是图片")
	}
	input, err := os.Open(row.Path)
	if err != nil {
		return "", err
	}
	defer input.Close()
	if _, _, err = image.DecodeConfig(input); err != nil {
		return "", errors.New("无效头像图片")
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	dir := "static/avatars"
	if err = os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	name := filepath.Join(dir, tools.Uuid()+ext)
	output, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(name)
		return "", errors.New("头像保存失败")
	}
	return "/" + filepath.ToSlash(name), nil
}
