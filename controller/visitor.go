package controller

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"goflylivechat/common"
	"goflylivechat/models"
	"goflylivechat/tools"
	"goflylivechat/ws"
	"strconv"
	"time"
)

//	func PostVisitor(c *gin.Context) {
//		name := c.PostForm("name")
//		avator := c.PostForm("avator")
//		toId := c.PostForm("to_id")
//		id := c.PostForm("id")
//		refer := c.PostForm("refer")
//		city := c.PostForm("city")
//		client_ip := c.PostForm("client_ip")
//		if name == "" || avator == "" || toId == "" || id == "" || refer == "" || city == "" || client_ip == "" {
//			c.JSON(200, gin.H{
//				"code": 400,
//				"msg":  "error",
//			})
//			return
//		}
//		kefuInfo := models.FindUser(toId)
//		if kefuInfo.ID == 0 {
//			c.JSON(200, gin.H{
//				"code": 400,
//				"msg":  "用户不存在",
//			})
//			return
//		}
//		models.CreateVisitor(name, avator, c.ClientIP(), toId, id, refer, city, client_ip)
//
//		userInfo := make(map[string]string)
//		userInfo["uid"] = id
//		userInfo["username"] = name
//		userInfo["avator"] = avator
//		msg := TypeMessage{
//			Type: "userOnline",
//			Data: userInfo,
//		}
//		str, _ := json.Marshal(msg)
//		kefuConns := kefuList[toId]
//		if kefuConns != nil {
//			for k, kefuConn := range kefuConns {
//				log.Println(k, "xxxxxxxx")
//				kefuConn.WriteMessage(websocket.TextMessage, str)
//			}
//		}
//		c.JSON(200, gin.H{
//			"code": 200,
//			"msg":  "ok",
//		})
//	}
func PostVisitorLogin(c *gin.Context) {
	if common.AppChatEnabled() {
		c.JSON(403, gin.H{"code": 403, "msg": "请从 App 内进入客服"})
		return
	}

	avator := ""
	userAgent := c.GetHeader("User-Agent")
	if tools.IsMobile(userAgent) {
		avator = "/static/images/1.png"
	} else {
		avator = "/static/images/2.png"
	}

	toId := c.PostForm("to_id")
	id := c.PostForm("visitor_id")

	if id == "" {
		id = tools.Uuid()
	}
	refer := c.PostForm("refer")
	name := "Guest"
	city := ""
	countryname, cityname := tools.GetCity("./config/GeoLite2-City.mmdb", c.ClientIP())
	if countryname != "" || cityname != "" {
		city = fmt.Sprintf("%s %s", countryname, cityname)
		name = fmt.Sprintf("%s Guest", city)
	}

	client_ip := c.ClientIP()
	extra := c.PostForm("extra")
	extraJson := tools.Base64Decode(extra)
	if extraJson != "" {
		var extraObj VisitorExtra
		err := json.Unmarshal([]byte(extraJson), &extraObj)
		if err == nil {
			if extraObj.VisitorName != "" {
				name = extraObj.VisitorName
			}
			if extraObj.VisitorAvatar != "" {
				avator = extraObj.VisitorAvatar
			}
		}
	}
	//log.Println(name,avator,c.ClientIP(),toId,id,refer,city,client_ip)
	if name == "" || avator == "" || toId == "" || id == "" || refer == "" || client_ip == "" {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "error",
		})
		return
	}
	visitor := models.FindVisitorByVistorId(id)
	if visitor.ID != 0 && visitor.ToId != "" {
		toId = visitor.ToId
	}
	kefuInfo := models.FindUser(toId)
	if kefuInfo.ID == 0 {
		c.JSON(200, gin.H{
			"code": 400,
			"msg":  "The customer service account does not exist",
		})
		return
	}
	var saveErr error
	if visitor.ID != 0 {
		//更新状态上线，保留转接后的客服归属。
		saveErr = models.UpdateVisitor(name, visitor.Avator, id, 1, c.ClientIP(), c.ClientIP(), refer, extra)
		if saveErr == nil {
			visitor = models.FindVisitorByVistorId(id)
		}
	} else {
		visitor, saveErr = models.CreateVisitor(name, avator, c.ClientIP(), toId, id, refer, city, client_ip, extra)
	}
	if saveErr != nil || visitor.ID == 0 {
		c.JSON(200, gin.H{"code": 500, "msg": "登录保存失败"})
		return
	}

	//各种通知
	go SendNoticeEmail(kefuInfo.Name, visitor.Name, " incoming!")
	//go SendAppGetuiPush(kefuInfo.Name, visitor.Name, visitor.Name+" incoming!")
	go SendVisitorLoginNotice(kefuInfo.Name, visitor.Name, visitor.Avator, visitor.Name+" incoming!", visitor.VisitorId)
	go ws.VisitorOnline(kefuInfo.Name, visitor)

	// 下发访客 token，后续取历史消息 / 未读数时用来证明「我就是这个访客」。
	visitorToken, err := tools.MakeToken(map[string]interface{}{
		"visitor_id":  visitor.VisitorId,
		"create_time": time.Now().Unix(),
	})
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "登录失败"})
		return
	}

	c.JSON(200, gin.H{
		"code": 200,
		"msg":  "ok",
		"result": gin.H{
			"id":           visitor.ID,
			"name":         visitor.Name,
			"avator":       visitor.Avator,
			"visitor_id":   visitor.VisitorId,
			"to_id":        visitor.ToId,
			"client_ip":    visitor.ClientIp,
			"city":         visitor.City,
			"refer":        visitor.Refer,
			"created_at":   visitor.CreatedAt,
			"last_message": visitor.LastMessage,
			"token":        visitorToken,
		},
	})
}
func GetVisitor(c *gin.Context) {
	visitorId := c.Query("visitorId")
	vistor := models.FindVisitorByVistorId(visitorId)
	if common.AppChatEnabled() && !visitorScopeAllows(c, visitorId) {
		c.JSON(403, gin.H{"code": 403, "msg": "无权访问该会话"})
		return
	}
	c.JSON(200, gin.H{
		"code":   200,
		"msg":    "ok",
		"result": vistor,
	})
}

// @Summary 获取访客列表接口
// @Produce  json
// @Accept multipart/form-data
// @Param page query   string true "分页"
// @Param token header string true "认证token"
// @Success 200 {object} controller.Response
// @Failure 200 {object} controller.Response
// @Router /visitors [get]
func GetVisitors(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pagesize, _ := strconv.Atoi(c.Query("pagesize"))
	if pagesize == 0 {
		pagesize = 10
	}
	kefuId, _ := c.Get("kefu_name")
	vistors := models.FindVisitorsByKefuId(uint(page), uint(pagesize), kefuId.(string))
	count := models.CountVisitorsByKefuId(kefuId.(string))
	c.JSON(200, gin.H{
		"code": 200,
		"msg":  "ok",
		"result": gin.H{
			"list":     vistors,
			"count":    count,
			"pagesize": common.PageSize,
		},
	})
}

// visitorScopeAllows 在 VisitorScope 放行后再确认归属：
// 访客只能访问自己，客服只能访问分配给自己的访客。
// 中间件已经保证两者必居其一，这里负责客服侧的归属判断。
func visitorScopeAllows(c *gin.Context, visitorID string) bool {
	if id, ok := c.Get("visitor_id"); ok && id == visitorID {
		return true
	}
	kefuName, ok := c.Get("kefu_name")
	if !ok {
		return false
	}
	return models.FindVisitorByVistorId(visitorID).ToId == kefuName
}

func GetVisitorMessageStatus(c *gin.Context) {
	visitorID := c.Query("visitor_id")
	if visitorID == "" {
		visitorID = c.Query("visitorId")
	}
	if visitorID == "" {
		c.JSON(200, gin.H{"code": 400, "msg": "visitor_id不能为空"})
		return
	}
	if !visitorScopeAllows(c, visitorID) {
		c.JSON(200, gin.H{"code": 403, "msg": "无权访问该会话"})
		return
	}
	unread, err := models.FindUnreadMessageNumByVisitorId(visitorID, incomingMessageType(c))
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "获取未读消息失败"})
		return
	}
	c.JSON(200, gin.H{"code": 200, "msg": "ok", "result": gin.H{"unread": unread}})
}

func MarkVisitorMessageRead(c *gin.Context) {
	visitorID := c.PostForm("visitor_id")
	if visitorID == "" {
		visitorID = c.Query("visitor_id")
	}
	if visitorID == "" {
		visitorID = c.Query("visitorId")
	}
	if visitorID == "" {
		c.JSON(200, gin.H{"code": 400, "msg": "visitor_id不能为空"})
		return
	}
	if !visitorScopeAllows(c, visitorID) {
		c.JSON(200, gin.H{"code": 403, "msg": "无权访问该会话"})
		return
	}
	lastID, err := strconv.ParseUint(c.PostForm("last_message_id"), 10, 32)
	if err != nil || lastID == 0 {
		c.JSON(200, gin.H{"code": 400, "msg": "last_message_id必须是已展示的消息ID"})
		return
	}
	if err := models.ReadMessageByVisitorId(visitorID, incomingMessageType(c), uint(lastID)); err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "更新已读消息失败"})
		return
	}
	c.JSON(200, gin.H{"code": 200, "msg": "ok"})
}

func incomingMessageType(c *gin.Context) string {
	if _, visitor := c.Get("visitor_id"); visitor {
		return "kefu"
	}
	return "visitor"
}

func GetKefuUnreadMessages(c *gin.Context) {
	rows, err := models.FindKefuUnreadMessages(c.GetString("kefu_name"))
	if err != nil {
		c.JSON(200, gin.H{"code": 500, "msg": "获取未读消息失败"})
		return
	}
	counts := make(map[string]uint, len(rows))
	var total uint
	for _, row := range rows {
		counts[row.VisitorId] = row.Unread
		total += row.Unread
	}
	c.JSON(200, gin.H{"code": 200, "msg": "ok", "result": gin.H{"counts": counts, "total": total}})
}

// @Summary 获取客服的在线访客列表接口
// @Produce  json
// @Success 200 {object} controller.Response
// @Failure 200 {object} controller.Response
// @Router /visitors_kefu_online [get]
func GetKefusVisitorOnlines(c *gin.Context) {
	kefuName, _ := c.Get("kefu_name")
	users := make([]*VisitorOnline, 0)
	visitorIds := make([]string, 0)
	for _, visitor := range ws.ClientList.Snapshot() {
		if visitor.RouteTo() != kefuName {
			continue
		}
		userInfo := new(VisitorOnline)
		userInfo.Uid = visitor.Id
		userInfo.Username = visitor.Name
		userInfo.Avator = visitor.Avator
		users = append(users, userInfo)
		visitorIds = append(visitorIds, visitor.Id)
	}

	//查询最新消息
	messages := models.FindLastMessage(visitorIds)
	temp := make(map[string]string, 0)
	for _, mes := range messages {
		temp[mes.VisitorId] = mes.Content
	}
	for _, user := range users {
		user.LastMessage = temp[user.Uid]
		if user.LastMessage == "" {
			user.LastMessage = "new visitor"
		}
	}

	c.JSON(200, gin.H{
		"code":   200,
		"msg":    "ok",
		"result": users,
	})
}
