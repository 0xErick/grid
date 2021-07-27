package main

import (
	"time"

	_ "github.com/mattn/go-sqlite3"

	"grid/core"
	"grid/orders"
	"grid/utils"
)

func main() {
	config, err := utils.GetConfig()
	if err != nil {
		return
	}

	// 读取账户ID
	AccountId, err := orders.GetAccounId(config.AccessKey, config.SecretKey, config.Host)
	if err != nil {
		utils.Error(err, "读取账户ID出错：")
		return
	}
	utils.Info(nil, "账户ID：", AccountId)

	// 开启循环
	timeTickerChan := time.Tick(time.Second * 1)
	for {

		var cstZone = time.FixedZone("CST", 8*3600) // 东八
		t4 := time.Now().In(cstZone).Hour()         //小时
		t5 := time.Now().Minute()                   //分钟
		t6 := time.Now().Second()                   //秒

		// 北京时间中午 11点半 准时 发送一次 当前资产估值
		if t4 == 11 && t5 == 30 && t6 == 0 {
			orders.GetCurrentMoney(config.AccessKey, config.SecretKey, config.Host,
				AccountId, config.Pair, config.WebhookURL)
		}

		unix := time.Now().Unix()

		// 15秒干一次
		if unix%15 == 0 {
			utils.Info(nil, unix, "每隔15秒执行一次")
			// 执行核心逻辑
			core.Handle(AccountId)
		}
		<-timeTickerChan
	}
}
