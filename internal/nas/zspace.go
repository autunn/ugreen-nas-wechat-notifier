package nas

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"nasnotify-go/internal/config"
	"nasnotify-go/internal/notify"
	"nasnotify-go/internal/utils"
)

// ZSpaceNotice 定义极空间的通知结构
type ZSpaceNotice struct {
	CreatedAt string `json:"created_at"`
	Content   string `json:"content"`
}

type ZSpaceResponse struct {
	Data struct {
		List []ZSpaceNotice `json:"list"`
	} `json:"data"`
}

// ProcessZSpace 极空间任务主函数
func ProcessZSpace() {
	if len(config.Config.ZSpace) == 0 {
		return
	}

	for _, cfg := range config.Config.ZSpace {
		ip, port := utils.SplitIpPort(cfg.IpPort, 5055)
		if !utils.HandleDeviceStatus("极空间", cfg.NotifyTypeName, ip, port) {
			continue
		}

		logFile := utils.DeviceLogFile("zspace", cfg.ID, ip, port)

		// 1. 获取最新通知
		notices, err := fetchZSpaceNotices(cfg.Cookie, ip, port, cfg.UseSSL)
		if err != nil {
			log.Printf("[极空间] 获取通知失败: %v\n", err)
			continue
		}

		// 2. 比对本地时间戳并过滤新消息
		lastTime := getLastZSpaceTime(logFile)
		var newNotices []ZSpaceNotice

		for _, notice := range notices {
			t, err := time.ParseInLocation("2006-01-02 15:04:05", notice.CreatedAt, time.Local)
			if err == nil && t.After(lastTime) {
				newNotices = append(newNotices, notice)
			}
		}

		// 3. 处理推送与保存
		fileInfo, err := os.Stat(logFile)
		isFirstRun := false
		if err != nil {
			isFirstRun = os.IsNotExist(err)
		} else {
			isFirstRun = fileInfo.Size() == 0
		}

		if isFirstRun {
			// 首次运行：保存所有数据并推送
			saveZSpaceNotices(newNotices, logFile)
			pushContent := buildZSpacePushContent(newNotices, cfg.NotifyTypeName)
			if pushContent != "" {
				notify.WechatPush(pushContent)
				log.Println("[极空间] 新增通知 (首次生成)")
			}
		} else if len(newNotices) > 0 {
			// 非首次运行：有更新数据，覆盖写入并推送
			saveZSpaceNotices(newNotices, logFile)
			pushContent := buildZSpacePushContent(newNotices, cfg.NotifyTypeName)
			if pushContent != "" {
				notify.WechatPush(pushContent)
				log.Println("[极空间] 覆盖日志，新增通知")
			}
		} else {
			log.Printf("[极空间] %s 没有新的通知\n", cfg.NotifyTypeName)
		}
	}
}

// fetchZSpaceNotices 请求极空间接口获取通知
func fetchZSpaceNotices(cookie, ip string, port int, useSSL bool) ([]ZSpaceNotice, error) {
	protocol := "http"
	if useSSL {
		protocol = "https"
	}
	apiURL := fmt.Sprintf("%s://%s:%d/action/list", protocol, ip, port)

	payload := url.Values{}
	payload.Set("type", "notify")
	payload.Set("num", "10")

	req, _ := http.NewRequest("POST", apiURL, strings.NewReader(payload.Encode()))
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// 忽略 HTTPS 证书校验 (对应 Python 的 verify=False)
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var zResp ZSpaceResponse
	json.Unmarshal(body, &zResp)

	return zResp.Data.List, nil
}

// getLastZSpaceTime 从本地日志获取最后一条记录的时间
func getLastZSpaceTime(filepath string) time.Time {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return time.Time{}
	}

	lines := strings.Split(string(content), "\n")
	var maxTime time.Time

	for _, line := range lines {
		parts := strings.SplitN(line, "：", 2)
		if len(parts) == 2 {
			if t, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(parts[0]), time.Local); err == nil {
				if t.After(maxTime) {
					maxTime = t
				}
			}
		}
	}
	return maxTime
}

// saveZSpaceNotices 覆盖保存通知到本地文件
func saveZSpaceNotices(notices []ZSpaceNotice, filepath string) {
	var builder strings.Builder
	for _, notice := range notices {
		builder.WriteString(fmt.Sprintf("%s：%s\n", notice.CreatedAt, notice.Content))
	}
	os.WriteFile(filepath, []byte(builder.String()), 0644)
}

// buildZSpacePushContent 生成供微信推送的文本内容
func buildZSpacePushContent(notices []ZSpaceNotice, typeName string) string {
	if len(notices) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("%s消息通知（共%d条）", typeName, len(notices)))
	for i, notice := range notices {
		content := strings.ReplaceAll(notice.Content, "\n", " ") // 去除消息内部换行以免破坏排版
		builder.WriteString(fmt.Sprintf("\n\n%d. %s", i+1, content))
	}
	return builder.String()
}
