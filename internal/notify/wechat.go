package notify

func WechatPush(content string) error {
	return ClawBotPush(content)
}
