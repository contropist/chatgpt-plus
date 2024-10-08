package chatimpl

// * +++++++++++++++++++++++++++++++++++++++++++++++++++++++++++
// * Copyright 2023 The Geek-AI Authors. All rights reserved.
// * Use of this source code is governed by a Apache-2.0 license
// * that can be found in the LICENSE file.
// * @Author yangjian102621@163.com
// * +++++++++++++++++++++++++++++++++++++++++++++++++++++++++++

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"geekai/core/types"
	"geekai/store/model"
	"geekai/store/vo"
	"geekai/utils"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type xunFeiResp struct {
	Header struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Sid     string `json:"sid"`
		Status  int    `json:"status"`
	} `json:"header"`
	Payload struct {
		Choices struct {
			Status int `json:"status"`
			Seq    int `json:"seq"`
			Text   []struct {
				Content string `json:"content"`
				Role    string `json:"role"`
				Index   int    `json:"index"`
			} `json:"text"`
		} `json:"choices"`
		Usage struct {
			Text struct {
				QuestionTokens   int `json:"question_tokens"`
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"text"`
		} `json:"usage"`
	} `json:"payload"`
}

var Model2URL = map[string]string{
	"general":     "v1.1",
	"generalv2":   "v2.1",
	"generalv3":   "v3.1",
	"generalv3.5": "v3.5",
}

// 科大讯飞消息发送实现

func (h *ChatHandler) sendXunFeiMessage(
	chatCtx []types.Message,
	req types.ApiRequest,
	userVo vo.User,
	ctx context.Context,
	session *types.ChatSession,
	role model.ChatRole,
	prompt string,
	ws *types.WsClient) error {
	promptCreatedAt := time.Now() // 记录提问时间
	var apiKey model.ApiKey
	var res *gorm.DB
	// use the bind key
	if session.Model.KeyId > 0 {
		res = h.DB.Where("id", session.Model.KeyId).Find(&apiKey)
	}
	// use the last unused key
	if apiKey.Id == 0 {
		res = h.DB.Where("platform", session.Model.Platform).Where("type", "chat").Where("enabled", true).Order("last_used_at ASC").First(&apiKey)
	}
	if res.Error != nil {
		return errors.New("抱歉😔😔😔，系统已经没有可用的 API KEY，请联系管理员！")
	}
	// 更新 API KEY 的最后使用时间
	h.DB.Model(&apiKey).UpdateColumn("last_used_at", time.Now().Unix())

	d := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}
	key := strings.Split(apiKey.Value, "|")
	if len(key) != 3 {
		utils.ReplyMessage(ws, "非法的 API KEY！")
		return nil
	}

	apiURL := strings.Replace(apiKey.ApiURL, "{version}", Model2URL[req.Model], 1)
	logger.Debugf("Sending %s request, ApiURL:%s, API KEY:%s, PROXY: %s, Model: %s", session.Model.Platform, apiURL, apiKey.Value, apiKey.ProxyURL, req.Model)
	wsURL, err := assembleAuthUrl(apiURL, key[1], key[2])
	//握手并建立websocket 连接
	conn, resp, err := d.Dial(wsURL, nil)
	if err != nil {
		logger.Error(readResp(resp) + err.Error())
		utils.ReplyMessage(ws, "请求讯飞星火模型 API 失败："+readResp(resp)+err.Error())
		return nil
	} else if resp.StatusCode != 101 {
		utils.ReplyMessage(ws, "请求讯飞星火模型 API 失败："+readResp(resp)+err.Error())
		return nil
	}

	data := buildRequest(key[0], req)
	fmt.Printf("%+v", data)
	fmt.Println(apiURL)
	err = conn.WriteJSON(data)
	if err != nil {
		utils.ReplyMessage(ws, "发送消息失败："+err.Error())
		return nil
	}

	replyCreatedAt := time.Now() // 记录回复时间
	// 循环读取 Chunk 消息
	var message = types.Message{}
	var contents = make([]string, 0)
	var content string
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			logger.Error("error with read message:", err)
			utils.ReplyMessage(ws, fmt.Sprintf("**数据读取失败：%s**", err))
			break
		}

		// 解析数据
		var result xunFeiResp
		err = json.Unmarshal(msg, &result)
		if err != nil {
			logger.Error("error with parsing JSON:", err)
			utils.ReplyMessage(ws, fmt.Sprintf("**解析数据行失败：%s**", err))
			return nil
		}

		if result.Header.Code != 0 {
			utils.ReplyMessage(ws, fmt.Sprintf("**请求 API 返回错误：%s**", result.Header.Message))
			return nil
		}

		content = result.Payload.Choices.Text[0].Content
		// 处理代码换行
		if len(content) == 0 {
			content = "\n"
		}
		contents = append(contents, content)
		// 第一个结果
		if result.Payload.Choices.Status == 0 {
			utils.ReplyChunkMessage(ws, types.WsMessage{Type: types.WsStart})
		}
		utils.ReplyChunkMessage(ws, types.WsMessage{
			Type:    types.WsMiddle,
			Content: utils.InterfaceToString(content),
		})

		if result.Payload.Choices.Status == 2 { // 最终结果
			_ = conn.Close() // 关闭连接
			break
		}

		select {
		case <-ctx.Done():
			utils.ReplyMessage(ws, "**用户取消了生成指令！**")
			return nil
		default:
			continue
		}

	}
	// 消息发送成功
	if len(contents) > 0 {
		h.saveChatHistory(req, prompt, contents, message, chatCtx, session, role, userVo, promptCreatedAt, replyCreatedAt)
	}
	return nil
}

// 构建 websocket 请求实体
func buildRequest(appid string, req types.ApiRequest) map[string]interface{} {
	return map[string]interface{}{
		"header": map[string]interface{}{
			"app_id": appid,
		},
		"parameter": map[string]interface{}{
			"chat": map[string]interface{}{
				"domain":      req.Model,
				"temperature": req.Temperature,
				"top_k":       int64(6),
				"max_tokens":  int64(req.MaxTokens),
				"auditing":    "default",
			},
		},
		"payload": map[string]interface{}{
			"message": map[string]interface{}{
				"text": req.Messages,
			},
		},
	}
}

// 创建鉴权 URL
func assembleAuthUrl(hostURL string, apiKey, apiSecret string) (string, error) {
	ul, err := url.Parse(hostURL)
	if err != nil {
		return "", err
	}

	date := time.Now().UTC().Format(time.RFC1123)
	signString := []string{"host: " + ul.Host, "date: " + date, "GET " + ul.Path + " HTTP/1.1"}
	//拼接签名字符串
	signStr := strings.Join(signString, "\n")
	sha := hmacWithSha256(signStr, apiSecret)

	authUrl := fmt.Sprintf("hmac username=\"%s\", algorithm=\"%s\", headers=\"%s\", signature=\"%s\"", apiKey,
		"hmac-sha256", "host date request-line", sha)
	//将请求参数使用base64编码
	authorization := base64.StdEncoding.EncodeToString([]byte(authUrl))
	v := url.Values{}
	v.Add("host", ul.Host)
	v.Add("date", date)
	v.Add("authorization", authorization)
	//将编码后的字符串url encode后添加到url后面
	return hostURL + "?" + v.Encode(), nil
}

// 使用 sha256 签名
func hmacWithSha256(data, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(data))
	encodeData := mac.Sum(nil)
	return base64.StdEncoding.EncodeToString(encodeData)
}

// 读取响应
func readResp(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("code=%d,body=%s", resp.StatusCode, string(b))
}
