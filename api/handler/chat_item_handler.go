package handler

// * +++++++++++++++++++++++++++++++++++++++++++++++++++++++++++
// * Copyright 2023 The Geek-AI Authors. All rights reserved.
// * Use of this source code is governed by a Apache-2.0 license
// * that can be found in the LICENSE file.
// * @Author yangjian102621@163.com
// * +++++++++++++++++++++++++++++++++++++++++++++++++++++++++++

import (
	"geekai/core/types"
	"geekai/store/model"
	"geekai/store/vo"
	"geekai/utils"
	"geekai/utils/resp"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// List 获取会话列表
func (h *ChatHandler) List(c *gin.Context) {
	if !h.IsLogin(c) {
		resp.SUCCESS(c)
		return
	}

	userId := h.GetLoginUserId(c)
	var items = make([]vo.ChatItem, 0)
	var chats []model.ChatItem
	h.DB.Where("user_id", userId).Order("id DESC").Find(&chats)
	if len(chats) == 0 {
		resp.SUCCESS(c, items)
		return
	}

	var roleIds = make([]uint, 0)
	var modelValues = make([]string, 0)
	for _, chat := range chats {
		roleIds = append(roleIds, chat.RoleId)
		modelValues = append(modelValues, chat.Model)
	}

	var roles []model.ChatRole
	var models []model.ChatModel
	roleMap := make(map[uint]model.ChatRole)
	modelMap := make(map[string]model.ChatModel)
	h.DB.Where("id IN ?", roleIds).Find(&roles)
	h.DB.Where("value IN ?", modelValues).Find(&models)
	for _, role := range roles {
		roleMap[role.Id] = role
	}
	for _, m := range models {
		modelMap[m.Value] = m
	}
	for _, chat := range chats {
		var item vo.ChatItem
		err := utils.CopyObject(chat, &item)
		if err == nil {
			item.Id = chat.Id
			item.Icon = roleMap[chat.RoleId].Icon
			item.ModelId = modelMap[chat.Model].Id
			items = append(items, item)
		}
	}
	resp.SUCCESS(c, items)
}

// Update 更新会话标题
func (h *ChatHandler) Update(c *gin.Context) {
	var data struct {
		ChatId string `json:"chat_id"`
		Title  string `json:"title"`
	}
	if err := c.ShouldBindJSON(&data); err != nil {
		resp.ERROR(c, types.InvalidArgs)
		return
	}
	res := h.DB.Model(&model.ChatItem{}).Where("chat_id = ?", data.ChatId).UpdateColumn("title", data.Title)
	if res.Error != nil {
		resp.ERROR(c, "Failed to update database")
		return
	}

	resp.SUCCESS(c, types.OkMsg)
}

// Clear 清空所有聊天记录
func (h *ChatHandler) Clear(c *gin.Context) {
	// 获取当前登录用户所有的聊天会话
	user, err := h.GetLoginUser(c)
	if err != nil {
		resp.NotAuth(c)
		return
	}

	var chats []model.ChatItem
	res := h.DB.Where("user_id = ?", user.Id).Find(&chats)
	if res.Error != nil {
		resp.ERROR(c, "No chats found")
		return
	}

	var chatIds = make([]string, 0)
	for _, chat := range chats {
		chatIds = append(chatIds, chat.ChatId)
		// 清空会话上下文
		h.ChatContexts.Delete(chat.ChatId)
	}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		res := h.DB.Where("user_id =?", user.Id).Delete(&model.ChatItem{})
		if res.Error != nil {
			return res.Error
		}

		res = h.DB.Where("user_id = ? AND chat_id IN ?", user.Id, chatIds).Delete(&model.ChatMessage{})
		if res.Error != nil {
			return res.Error
		}
		return nil
	})

	if err != nil {
		logger.Errorf("Error with delete chats: %+v", err)
		resp.ERROR(c, "Failed to remove chat from database.")
		return
	}

	resp.SUCCESS(c, types.OkMsg)
}

// History 获取聊天历史记录
func (h *ChatHandler) History(c *gin.Context) {
	chatId := c.Query("chat_id") // 会话 ID
	var items []model.ChatMessage
	var messages = make([]vo.HistoryMessage, 0)
	res := h.DB.Where("chat_id = ?", chatId).Find(&items)
	if res.Error != nil {
		resp.ERROR(c, "No history message")
		return
	} else {
		for _, item := range items {
			var v vo.HistoryMessage
			err := utils.CopyObject(item, &v)
			v.CreatedAt = item.CreatedAt.Unix()
			v.UpdatedAt = item.UpdatedAt.Unix()
			if err == nil {
				messages = append(messages, v)
			}
		}
	}

	resp.SUCCESS(c, messages)
}

// Remove 删除会话
func (h *ChatHandler) Remove(c *gin.Context) {
	chatId := h.GetTrim(c, "chat_id")
	if chatId == "" {
		resp.ERROR(c, types.InvalidArgs)
		return
	}
	user, err := h.GetLoginUser(c)
	if err != nil {
		resp.NotAuth(c)
		return
	}

	res := h.DB.Where("user_id = ? AND chat_id = ?", user.Id, chatId).Delete(&model.ChatItem{})
	if res.Error != nil {
		resp.ERROR(c, "Failed to update database")
		return
	}

	// 删除当前会话的聊天记录
	res = h.DB.Where("user_id = ? AND chat_id =?", user.Id, chatId).Delete(&model.ChatItem{})
	if res.Error != nil {
		resp.ERROR(c, "Failed to remove chat from database.")
		return
	}

	// TODO: 是否要删除 MidJourney 绘画记录和图片文件？

	// 清空会话上下文
	h.ChatContexts.Delete(chatId)
	resp.SUCCESS(c, types.OkMsg)
}

// Detail 对话详情，用户导出对话
func (h *ChatHandler) Detail(c *gin.Context) {
	chatId := h.GetTrim(c, "chat_id")
	if utils.IsEmptyValue(chatId) {
		resp.ERROR(c, "Invalid chatId")
		return
	}

	var chatItem model.ChatItem
	res := h.DB.Where("chat_id = ?", chatId).First(&chatItem)
	if res.Error != nil {
		resp.ERROR(c, "No chat found")
		return
	}

	// 填充角色名称
	var role model.ChatRole
	res = h.DB.Where("id", chatItem.RoleId).First(&role)
	if res.Error != nil {
		resp.ERROR(c, "Role not found")
		return
	}

	var chatItemVo vo.ChatItem
	err := utils.CopyObject(chatItem, &chatItemVo)
	if err != nil {
		resp.ERROR(c, err.Error())
		return
	}
	chatItemVo.RoleName = role.Name
	resp.SUCCESS(c, chatItemVo)
}
