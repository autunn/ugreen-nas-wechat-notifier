package nas

import (
	crand "crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"nasnotify-go/internal/config"
	"nasnotify-go/internal/crypto"
	"nasnotify-go/internal/notify"
	"nasnotify-go/internal/utils"
)

// fnOs 专用的时间格式
const fnosTimeFormat = "2006-01-02T15:04:05Z"

// FnOsClient 飞牛 WebSocket 客户端
type FnOsClient struct {
	conn     *websocket.Conn
	pub      string
	si       string
	aesKey   string
	iv       []byte
	backId   string
	signKey  string
	reqIndex int
	pending  map[string]chan map[string]interface{}
	mu       sync.Mutex
}

// ProcessFnOs 飞牛任务主函数
func ProcessFnOs() {
	devices := config.GetConfigSnapshot().FnOs
	if len(devices) == 0 {
		return
	}

	for _, cfg := range devices {
		ip, port := utils.SplitIpPort(cfg.Server, 5666)
		if !utils.HandleDeviceStatus("飞牛", cfg.NotifyTypeName, ip, port) {
			continue
		}

		logFile := utils.DeviceLogFile("fnos", cfg.ID, ip, port)

		client := NewFnOsClient()
		serverAddress := net.JoinHostPort(ip, strconv.Itoa(port))
		err := client.Connect(serverAddress, cfg.UseSSL, cfg.Cookie)
		if err != nil {
			log.Printf("[飞牛] 连接失败: %v\n", err)
			continue
		}

		// 1. 获取公钥
		err = client.GetRSAPub()
		if err != nil {
			log.Printf("[飞牛] 获取公钥失败: %v\n", err)
			client.Close()
			continue
		}

		// 2. 登录鉴权
		err = client.Login(cfg.Username, cfg.Password)
		if err != nil {
			log.Printf("[飞牛] 登录失败: %v\n", err)
			client.Close()
			continue
		}

		// 3. 获取通知列表
		resp, err := client.Request("notify.list", map[string]interface{}{"page": 1, "lastId": 0})
		client.Close() // 拿完数据就可以断开连接了

		if err != nil {
			log.Printf("[飞牛] 获取通知失败: %v\n", err)
			continue
		}

		notifyListInterface, ok := resp["notifyList"].([]interface{})
		if !ok {
			log.Printf("[飞牛] 通知列表解析失败\n")
			continue
		}

		// 4. 比对本地时间戳并过滤新消息
		lastTime := getLastFnOsTime(logFile)
		var newNotices []map[string]interface{}

		for _, item := range notifyListInterface {
			notice, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			dtStr, ok := notice["datetime"].(string)
			if !ok {
				continue
			}
			t, err := time.Parse(fnosTimeFormat, dtStr)
			if err == nil && t.After(lastTime) {
				newNotices = append(newNotices, notice)
			}
		}

		// 5. 排序、保存并推送
		if len(newNotices) > 0 {
			// 按时间升序排序，保证写入日志顺序正确
			sort.Slice(newNotices, func(i, j int) bool {
				t1, _ := time.Parse(fnosTimeFormat, getNoticeString(newNotices[i], "datetime"))
				t2, _ := time.Parse(fnosTimeFormat, getNoticeString(newNotices[j], "datetime"))
				return t1.Before(t2)
			})

			fileInfo, err := os.Stat(logFile)
			isFirstRun := false
			if err != nil {
				isFirstRun = os.IsNotExist(err)
			} else {
				isFirstRun = fileInfo.Size() == 0
			}

			if isFirstRun && len(newNotices) > 10 {
				newNotices = newNotices[len(newNotices)-10:] // 首次最多推 10 条
			}

			saveFnOsNotices(newNotices, logFile)
			pushContent := buildFnOsPushContent(newNotices, cfg.NotifyTypeName)
			if pushContent != "" {
				notify.WechatPush(pushContent)
				log.Printf("[飞牛] 发现 %d 条新通知并已推送\n", len(newNotices))
			}
		} else {
			log.Printf("[飞牛] %s 没有新的通知\n", cfg.NotifyTypeName)
		}
	}
}

// ----------------- WebSocket 核心逻辑 -----------------

func NewFnOsClient() *FnOsClient {
	return &FnOsClient{
		backId:  "0000000000000000",
		pending: make(map[string]chan map[string]interface{}),
	}
}

func (c *FnOsClient) Connect(server string, useSSL bool, cookie string) error {
	protocol := "ws"
	if useSSL {
		protocol = "wss"
	}
	u := fmt.Sprintf("%s://%s/websocket?type=main", protocol, server)

	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	header := http.Header{}
	if cookie != "" {
		header.Set("Cookie", cookie)
	} else if useSSL {
		header.Set("Cookie", "mode=relay; language=zh")
	}

	conn, _, err := dialer.Dial(u, header)
	if err != nil {
		return err
	}
	c.conn = conn

	// 启动读取协程
	go c.readLoop()
	return nil
}

func (c *FnOsClient) readLoop() {
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var data map[string]interface{}
		if err := json.Unmarshal(message, &data); err == nil {
			if reqid, ok := data["reqid"].(string); ok {
				c.mu.Lock()
				if ch, exists := c.pending[reqid]; exists {
					ch <- data
					delete(c.pending, reqid)
				}
				c.mu.Unlock()
			}
		}
	}
}

func (c *FnOsClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

func (c *FnOsClient) generateReqId() string {
	c.reqIndex++
	ts := fmt.Sprintf("%08x", time.Now().Unix())
	idx := fmt.Sprintf("%04x", c.reqIndex)
	return ts + c.backId + idx
}

func (c *FnOsClient) Request(reqType string, params map[string]interface{}) (map[string]interface{}, error) {
	reqid := c.generateReqId()

	payload := map[string]interface{}{
		"req":   reqType,
		"reqid": reqid,
	}
	for k, v := range params {
		payload[k] = v
	}

	// 针对登录进行特殊加密
	if reqType == "user.login" {
		jsonData, _ := json.Marshal(payload)

		// RSA 加密 AES 密钥
		rsaEnc, _ := crypto.RsaEncrypt(c.pub, c.aesKey)
		// AES 加密 Payload
		aesEnc, _ := crypto.AesEncrypt(string(jsonData), c.aesKey, c.iv)

		payload = map[string]interface{}{
			"req":   "encrypted",
			"reqid": reqid, // 透传 reqid 方便路由
			"iv":    base64.StdEncoding.EncodeToString(c.iv),
			"rsa":   rsaEnc,
			"aes":   aesEnc,
		}
	}

	var messageStr string
	jsonStr, _ := json.Marshal(payload)

	// 需要签名的请求
	if reqType != "encrypted" && reqType != "util.crypto.getRSAPub" && c.signKey != "" {
		sig, _ := crypto.GetSignature(string(jsonStr), c.signKey)
		messageStr = sig + string(jsonStr)
	} else {
		messageStr = string(jsonStr)
	}

	ch := make(chan map[string]interface{}, 1)
	c.mu.Lock()
	c.pending[reqid] = ch
	c.mu.Unlock()

	c.conn.WriteMessage(websocket.TextMessage, []byte(messageStr))

	select {
	case resp := <-ch:
		if errVal, ok := resp["errno"]; ok && errVal != nil {
			return nil, fmt.Errorf("服务器返回错误码: %v", errVal)
		}
		return resp, nil
	case <-time.After(10 * time.Second):
		c.mu.Lock()
		delete(c.pending, reqid)
		c.mu.Unlock()
		return nil, fmt.Errorf("请求超时: %s", reqType)
	}
}

func (c *FnOsClient) GetRSAPub() error {
	resp, err := c.Request("util.crypto.getRSAPub", nil)
	if err != nil {
		return err
	}
	pub, ok := resp["pub"].(string)
	if !ok || pub == "" {
		return fmt.Errorf("missing RSA public key")
	}
	si, ok := resp["si"].(string)
	if !ok || si == "" {
		return fmt.Errorf("missing security identifier")
	}
	c.pub = pub
	c.si = si
	return nil
}

func (c *FnOsClient) Login(username, password string) error {
	c.aesKey = generateRandomString(32)
	c.iv = make([]byte, 16)
	if _, err := crand.Read(c.iv); err != nil {
		return err
	}

	resp, err := c.Request("user.login", map[string]interface{}{
		"user":       username,
		"password":   password,
		"stay":       true,
		"deviceType": "Browser",
		"deviceName": "fnos-go-auth",
		"si":         c.si,
	})
	if err != nil {
		return err
	}

	if bid, ok := resp["backId"].(string); ok {
		c.backId = bid
	} else {
		return fmt.Errorf("missing backId")
	}
	if sec, ok := resp["secret"].(string); ok {
		signKey, err := crypto.AesDecrypt(sec, c.aesKey, c.iv)
		if err != nil {
			return err
		}
		c.signKey = signKey
	} else {
		return fmt.Errorf("missing secret")
	}
	return nil
}

// ----------------- 辅助与文件方法 -----------------

func generateRandomString(n int) string {
	const letters = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	if _, err := crand.Read(b); err != nil {
		for i := range b {
			b[i] = letters[(int(time.Now().UnixNano())+i)%len(letters)]
		}
		return string(b)
	}
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}

func getLastFnOsTime(file string) time.Time {
	content, err := os.ReadFile(file)
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

func saveFnOsNotices(notices []map[string]interface{}, file string) {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	for _, notice := range notices {
		dtStr := getNoticeString(notice, "datetime")
		content := getNoticeString(notice, "content")
		if dtStr == "" || content == "" {
			continue
		}

		t, _ := time.Parse(fnosTimeFormat, dtStr)
		// 飞牛接口返回的是 UTC 时间，转成本地（北京）时间记录
		localTime := t.In(time.FixedZone("CST", 8*3600))
		f.WriteString(fmt.Sprintf("%s：%s\n", localTime.Format("2006-01-02 15:04:05"), content))
	}
}

func buildFnOsPushContent(notices []map[string]interface{}, typeName string) string {
	if len(notices) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("%s消息通知（共%d条）", typeName, len(notices)))
	for i, notice := range notices {
		content := getNoticeString(notice, "content")
		if content == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("\n\n%d. %s", i+1, content))
	}
	return builder.String()
}

func getNoticeString(notice map[string]interface{}, key string) string {
	if notice == nil {
		return ""
	}
	val, ok := notice[key]
	if !ok {
		return ""
	}
	s, ok := val.(string)
	if !ok {
		return ""
	}
	return s
}
