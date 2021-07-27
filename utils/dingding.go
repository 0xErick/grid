package utils

import (
	"github.com/royeo/dingrobot"
)

func Notify(WebhookURL string, message string) {
	webhook := WebhookURL
	robot := dingrobot.NewRobot(webhook)
	content := message
	atMobiles := []string{}
	isAtAll := false
	err := robot.SendText(content, atMobiles, isAtAll)
	if err != nil {
		Error(err, "dingding 通知")
	}

}
