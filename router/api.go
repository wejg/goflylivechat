package router

import (
	"github.com/gin-gonic/gin"
	"goflylivechat/controller"
	"goflylivechat/middleware"
	"goflylivechat/ws"
)

func InitApiRouter(engine *gin.Engine) {
	//路由分组
	v2 := engine.Group("/2")
	{
		//发送单条信息
		v2.POST("/message", middleware.Ipblack, controller.SendMessageV2)
		//关闭连接
		v2.GET("/message_close", middleware.JwtApiMiddleware, controller.SendCloseMessageV2)
		//分页查询消息
		v2.GET("/messagesPages", middleware.VisitorScope, controller.GetMessagespages)
	}
	engine.POST("/check", controller.LoginCheckPass)

	engine.GET("/userinfo", middleware.JwtApiMiddleware, controller.GetKefuInfoAll)
	engine.POST("/register", middleware.Ipblack, controller.PostKefuRegister)
	engine.POST("/install", controller.PostInstall)
	//前后聊天
	engine.GET("/ws_kefu", middleware.JwtApiMiddleware, ws.NewKefuServer)
	engine.GET("/ws_visitor", middleware.Ipblack, ws.NewVisitorServer)

	engine.GET("/message_notice", middleware.JwtApiMiddleware, controller.SendVisitorNotice)
	//上传文件
	engine.POST("/uploadimg", middleware.Ipblack, controller.UploadImg)
	//上传文件
	engine.POST("/uploadfile", middleware.Ipblack, controller.UploadFile)
	//获取未读消息数
	engine.GET("/message_status", middleware.VisitorScope, controller.GetVisitorMessageStatus)
	engine.GET("/message_unread", middleware.JwtApiMiddleware, controller.GetKefuUnreadMessages)
	//设置消息已读
	engine.POST("/message_status", middleware.VisitorScope, controller.MarkVisitorMessageRead)

	//获取客服信息
	engine.POST("/kefuinfo_client", middleware.JwtApiMiddleware, controller.PostKefuClient)
	engine.GET("/kefuinfo", middleware.JwtApiMiddleware, controller.GetKefuInfo)
	engine.GET("/kefuinfo_setting", middleware.JwtApiMiddleware, controller.GetKefuInfoSetting)
	engine.POST("/kefuinfo", middleware.JwtApiMiddleware, controller.PostKefuInfo)
	//删除客服账号属于管理操作
	engine.DELETE("/kefuinfo", middleware.JwtApiMiddleware, middleware.RbacAuth, controller.DeleteKefuInfo)
	engine.GET("/kefulist", middleware.JwtApiMiddleware, middleware.RbacAuth, controller.GetKefuList)
	engine.GET("/other_kefulist", middleware.JwtApiMiddleware, controller.GetOtherKefuList)
	engine.GET("/trans_kefu", middleware.JwtApiMiddleware, controller.PostTransKefu)
	//改自己的密码和头像，每个客服都要能用
	engine.POST("/modifypass", middleware.JwtApiMiddleware, controller.PostKefuPass)
	engine.POST("/modifyavator", middleware.JwtApiMiddleware, controller.PostKefuAvator)
	//角色列表
	engine.GET("/roles", middleware.JwtApiMiddleware, middleware.RbacAuth, controller.GetRoleList)
	engine.POST("/role", middleware.JwtApiMiddleware, middleware.RbacAuth, controller.PostRole)

	engine.GET("/visitors_kefu_online", middleware.JwtApiMiddleware, controller.GetKefusVisitorOnlines)
	engine.POST("/visitor_login", middleware.Ipblack, controller.PostVisitorLogin)
	engine.GET("/visitor", middleware.JwtApiMiddleware, controller.GetVisitor)
	engine.GET("/visitors", middleware.JwtApiMiddleware, controller.GetVisitors)
	engine.GET("/statistics", middleware.JwtApiMiddleware, controller.GetStatistics)
	//前台接口
	engine.GET("/about", controller.GetAbout)
	engine.POST("/about", middleware.JwtApiMiddleware, middleware.RbacAuth, controller.PostAbout)
	engine.GET("/aboutpages", middleware.JwtApiMiddleware, middleware.RbacAuth, controller.GetAbouts)
	engine.GET("/notice", controller.GetNotice)
	engine.POST("/ipblack", middleware.JwtApiMiddleware, controller.PostIpblack)
	engine.DELETE("/ipblack", middleware.JwtApiMiddleware, controller.DelIpblack)
	engine.GET("/ipblacks_all", middleware.JwtApiMiddleware, middleware.RbacAuth, controller.GetIpblacks)
	engine.GET("/ipblacks", middleware.JwtApiMiddleware, controller.GetIpblacksByKefuId)
	//配置是按 user_id 存的，每个客服配置自己的
	engine.GET("/configs", middleware.JwtApiMiddleware, controller.GetConfigs)
	engine.POST("/config", middleware.JwtApiMiddleware, controller.PostConfig)
	engine.GET("/config", controller.GetConfig)
	engine.GET("/autoreply", controller.GetAutoReplys)
	engine.GET("/replys", middleware.JwtApiMiddleware, controller.GetReplys)
	//快捷回复也是按 user_id 存的
	engine.POST("/reply", middleware.JwtApiMiddleware, controller.PostReply)
	engine.POST("/reply_content", middleware.JwtApiMiddleware, controller.PostReplyContent)
	engine.POST("/reply_content_save", middleware.JwtApiMiddleware, controller.PostReplyContentSave)
	engine.DELETE("/reply_content", middleware.JwtApiMiddleware, controller.DelReplyContent)
	engine.DELETE("/reply", middleware.JwtApiMiddleware, controller.DelReplyGroup)
	engine.POST("/reply_search", middleware.JwtApiMiddleware, controller.PostReplySearch)
	//客服路由分组
	kefuGroup := engine.Group("/kefu")
	kefuGroup.Use(middleware.JwtApiMiddleware)
	{
		kefuGroup.GET("/chartStatistics", controller.GetChartStatistic)
		kefuGroup.POST("/message", controller.SendKefuMessage)
	}
	//微信接口
	engine.GET("/micro_program", middleware.JwtApiMiddleware, controller.GetCheckWeixinSign)
}
