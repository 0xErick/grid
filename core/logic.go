package core

import (
	"database/sql"
	"errors"
	"fmt"
	"grid/orders"
	"grid/utils"
	"math"
	"strconv"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/huobirdcenter/huobi_golang/pkg/client"
	"github.com/huobirdcenter/huobi_golang/pkg/model/order"
	_ "github.com/mattn/go-sqlite3"
	"github.com/shopspring/decimal"
)

type Order struct {
	Id        int
	Orderid   string
	Symbol    string
	Price     float64
	Amount    float64
	State     string
	Limittype string
	Createdat int
}

func Handle(AccountId string) {

	utils.Info(nil, "进入 handle")

	if !initDB() {
		utils.Info(nil, "数据库初始化失败")
		return
	}
	utils.Info(nil, "数据库初始化完成")

	finished, unfinished, err := getCount()

	if err != nil {
		utils.Error(err, "getCount")
		return
	}
	utils.Info(nil, "unfinished count ", unfinished)
	utils.Info(nil, "finished count ", finished)

	config, err := utils.GetConfig()
	if err != nil {
		utils.Error(err, "配置读取失败")
		return
	}

	// 读取火币上的开放订单
	openOrders, err := orders.GetOpenOrders(config.AccessKey, config.SecretKey,
		config.Host, AccountId, config.Pair)

	if err != nil {
		utils.Error(err, "getOpenOrders")
		return
	}

	// 初次买入
	if unfinished == 0 && finished == 0 {
		utils.Info(nil, "初次买入 ")
		initBuy(AccountId, openOrders)
	} else {
		// 成交事件
		tradeEvent(AccountId, openOrders)
	}

	// 补仓买入
	// 当前价格是否低于开放订单最低价格的  且开放订单数量少于4个

	if len(openOrders) > 0 && len(openOrders) < 4 {
		latestPrice, err := orders.GetLatestTrade(config.Host, config.Pair)
		minPriceInOpenOrder := 0.0
		if err != nil {
			utils.Error(err, "读取最近成交价格出错")
		} else {
			// 买入价格：开放卖出订单中的最低价格 * (100-rate-rate) /100
			// 买入数量：AmountPerTrade/买入价格
			for _, o := range openOrders {
				if o.Type == "sell-limit" {
					if minPriceInOpenOrder == 0 {
						minPriceInOpenOrder = getFloat(o.Price)
					} else {
						if getFloat(o.Price) < minPriceInOpenOrder {
							minPriceInOpenOrder = getFloat(o.Price)
						}
					}
				}
			}

			if minPriceInOpenOrder > 0 {
				targetPriceD := mul(decimal.NewFromFloat(minPriceInOpenOrder),
					decimal.NewFromFloat((100-config.Rate-config.Rate)/100*math.Pow(10, config.PriceAccuracy)))
				targetPrice := float64(getInt(targetPriceD)) / math.Pow(10, config.PriceAccuracy)
				amountD := decimal.NewFromFloat(config.AmountPerTrade / targetPrice * math.Pow(10, config.AmountAccuracy))
				amount := float64(getInt(amountD)) / math.Pow(10, config.AmountAccuracy)
				if getFloat(latestPrice) < targetPrice {
					if targetPrice > config.MaxPrice || targetPrice < config.MinPrice {
						utils.Notify(config.WebhookURL, fmt.Sprintf("下单出错，方向：buy-limit，市价异常,%s 市价: %s usdt",
							config.Pair, strconv.FormatFloat(targetPrice, 'f', -1, 64)))
					} else {
						_, err := makeOrder(AccountId, "buy-limit",
							strconv.FormatFloat(targetPrice, 'f', -1, 64),
							strconv.FormatFloat(amount, 'f', -1, 64))
						if err != nil {
							utils.Error(err, "下单出错")
							utils.Notify(config.WebhookURL, "下单出错，方向：buy-limit，请登录服务器查看日志")
						}
					}

				}
			}
		}
	}

}

func initDB() bool {
	config, err := utils.GetConfig()
	if err != nil {
		utils.Error(err, "配置读取失败")
		return false
	}

	// SQLITE
	db, err := sql.Open("sqlite3", config.DBPath)

	if err != nil {
		utils.Error(err, "sqlite open")
		return false
	}

	defer db.Close()
	sql_table := `
			CREATE TABLE IF NOT EXISTS "orders" (
	"id"	INTEGER,
	"orderid"	TEXT,
	"symbol"	TEXT,
	"price"	REAL,
	"amount"	REAL,
	"limittype"	TEXT,
	"state"	TEXT,
	"createdat" INTEGER,
	PRIMARY KEY("id" AUTOINCREMENT)

		);
	`
	_, err = db.Exec(sql_table) //执行数据表

	if err != nil {
		utils.Error(err, "db exec initDB")
	}
	return err == nil
}

func getCount() (finished int32, unfinished int32, err error) {

	config, err := utils.GetConfig()
	if err != nil {
		utils.Error(err, "配置读取失败")
		return 0, 0, err
	}

	// SQLITE
	db, err := sql.Open("sqlite3", config.DBPath)

	if err != nil {
		utils.Error(err, "sqlite open")
		return 0, 0, err
	}

	defer db.Close()

	rows, err := db.Query("SELECT COUNT(*) as count FROM orders WHERE state='unfinished'")
	if err != nil {
		utils.Error(err, "查询数量出错")
	}

	for rows.Next() {
		err := rows.Scan(&unfinished)
		if err != nil {
			return 0, 0, err
		}
	}

	rows2, err := db.Query("SELECT COUNT(*) as count FROM orders WHERE state='finished'")
	if err != nil {
		utils.Error(err, "查询数量出错")
	}

	for rows2.Next() {
		err := rows2.Scan(&finished)
		if err != nil {
			return 0, 0, err
		}
	}

	return finished, unfinished, nil
}

func initBuy(AccountId string, openOrders []order.OpenOrder) {
	config, err := utils.GetConfig()
	if err != nil {
		utils.Error(err, "配置读取失败")
		return
	}

	if len(openOrders) == 0 {
		latestPrice, err := orders.GetLatestTrade(config.Host, config.Pair)
		if err != nil {
			utils.Error(err, "读取最近成交价格出错")
			return
		}

		// 买入价格：市价 * (100-rate)/100
		// 买入数量：AmountPerTrade/买入价格
		firstBuyPriceD := mul(
			latestPrice,
			decimal.NewFromFloat(
				(100-config.Rate)/100*
					math.Pow(10, config.PriceAccuracy)))

		price := float64(getInt(firstBuyPriceD)) / math.Pow(10, config.PriceAccuracy)

		amountD := decimal.NewFromFloat(config.AmountPerTrade / price *
			math.Pow(10, config.AmountAccuracy))

		amount := float64(getInt(amountD)) / math.Pow(10, config.AmountAccuracy)

		if price > config.MaxPrice || price < config.MinPrice {
			utils.Notify(config.WebhookURL,
				fmt.Sprintf("下单出错，方向：buy-limit，市价异常,%s 市价: %s usdt",
					config.Pair, strconv.FormatFloat(price, 'f', -1, 64)))
		} else {
			_, err := makeOrder(AccountId, "buy-limit",
				strconv.FormatFloat(price, 'f', -1, 64),
				strconv.FormatFloat(amount, 'f', -1, 64))
			if err != nil {
				utils.Error(err, "初次下单出错")
				utils.Notify(config.WebhookURL,
					"下单出错，方向：buy-limit，请登录服务器查看日志")
			}
		}
	}

}

func makeOrder(AccountId string, TradeType string, Price string, Amount string) (string, error) {

	config, err := utils.GetConfig()
	if err != nil {
		utils.Error(err, "配置读取失败")
		return "", err
	}
	client := new(client.OrderClient).Init(config.AccessKey, config.SecretKey, config.Host)
	request := order.PlaceOrderRequest{
		AccountId: AccountId,
		Type:      TradeType, //"buy-limit", "sell-limit",
		Source:    "spot-api",
		Symbol:    config.Pair,
		Price:     Price,
		Amount:    Amount,
	}
	resp, err := client.PlaceOrder(&request)

	if err != nil {
		return "", err
	}

	// SQLITE
	db, err := sql.Open("sqlite3", config.DBPath)

	if err != nil {
		utils.Error(err, "sqlite open")
		return "", err
	}

	defer db.Close()

	if err != nil {
		return "", err
	} else {
		switch resp.Status {
		case "ok":
			stmt, err := db.Prepare("INSERT INTO orders(orderid, symbol,price,amount,limittype,state,createdat)  values(?, ?,?,?,?,?,?)")
			if err != nil {
				orders.CancelOrderById(config.AccessKey, config.SecretKey, config.Host, resp.Data)
				utils.Notify(config.WebhookURL, fmt.Sprintf("stmt 构造出错，orderid: %s", resp.Data))
				return "", err
			} else {
				_, err := stmt.Exec(resp.Data, config.Pair, Price, Amount, TradeType, "unfinished", time.Now().Unix())
				if err != nil {
					orders.CancelOrderById(config.AccessKey, config.SecretKey, config.Host, resp.Data)
					utils.Notify(config.WebhookURL, fmt.Sprintf("stmt 构造出错，orderid: %s", resp.Data))
					return "", err
				}
				return resp.Data, nil
			}
		case "error":
			return "", errors.New(resp.ErrorMessage)
		}

	}
	return "", errors.New("未知错误")
}

func mul(d1 decimal.Decimal, d2 decimal.Decimal) decimal.Decimal {
	return d1.Mul(d2)
}

// int
func getInt(d decimal.Decimal) int64 {
	return d.IntPart()
}

// float
func getFloat(d decimal.Decimal) float64 {
	f, exact := d.Float64()
	if !exact {
		return f
	}
	return f
}

// 成交事件
func tradeEvent(AccountId string, openOrders []order.OpenOrder) {
	config, err := utils.GetConfig()
	if err != nil {
		utils.Error(err, "配置读取失败")
		return
	}

	// SQLITE
	db, err := sql.Open("sqlite3", config.DBPath)

	if err != nil {
		utils.Error(err, "sqlite open")
		return
	}

	defer db.Close()

	// 读取 sqlite3 中的下单数据
	order_rows, err := db.Query("SELECT * FROM orders where state=='unfinished'")
	if err != nil {
		utils.Error(err, "sqlite rows 读取出错")

	}

	orders := []Order{}

	for order_rows.Next() {
		var id int
		var orderid string
		var symbol string
		var price float64
		var amount float64
		var state string
		var limittype string
		var createdat int

		err = order_rows.Scan(&id, &orderid, &symbol, &price, &amount, &limittype, &state, &createdat)

		if err != nil {
			utils.Error(err, "order_rows.Scan")
			return
		}
		orders = append(orders, Order{Id: id, Orderid: orderid, Symbol: symbol, Price: price, Amount: amount, State: state, Limittype: limittype, Createdat: createdat})

	}
	isMakeSellOrder := false
	isMakeBuyOrder := false
	for _, o := range orders {
		isFullfill := true
		for _, oo := range openOrders {
			if strconv.Itoa(int(oo.Id)) == o.Orderid {
				isFullfill = false
			}
		}

		if isFullfill {
			utils.Info(nil, "订单完成", o.Orderid)
			stmt, err := db.Prepare("update orders set state=? where orderid=?")
			if err != nil {
				utils.Error(err, "更新订单状态 prepare")
				return
			}

			_, err = stmt.Exec("finished", o.Orderid)
			if err != nil {

				utils.Error(err, "更新订单状态 exec")
				return
			}
			utils.Info(nil, "即将挂单", o.Orderid, o.Limittype, isMakeBuyOrder, isMakeSellOrder)

			// 挂卖单
			if o.Limittype == "buy-limit" && !isMakeSellOrder {

				// 最近一个是买单成交，则开启一个卖单，
				// 卖出价格：最近成交价 * (100+rate)/100
				// 卖出数量 = 最近成交的买入数量 *0.995  //考虑 交易手续费损失
				firstBuyPriceD := mul(decimal.NewFromFloat(o.Price),
					decimal.NewFromFloat((100+config.Rate)/100*math.Pow(10, config.PriceAccuracy)))
				price := float64(getInt(firstBuyPriceD)) / math.Pow(10, config.PriceAccuracy)
				amountD := decimal.NewFromFloat(o.Amount * 0.995 * math.Pow(10, config.AmountAccuracy))
				amount := float64(getInt(amountD)) / math.Pow(10, config.AmountAccuracy)

				if price > config.MaxPrice || price < config.MinPrice {
					utils.Notify(config.WebhookURL,
						fmt.Sprintf("下单出错，方向：sell-limit，市价异常,%s 市价: %s usdt",
							config.Pair, strconv.FormatFloat(price, 'f', -1, 64)))
					return
				} else {
					_, err := makeOrder(
						AccountId,
						"sell-limit",
						strconv.FormatFloat(price, 'f', -1, 64),
						strconv.FormatFloat(amount, 'f', -1, 64))
					if err != nil {
						utils.Error(err, "下单出错")
						utils.Notify(config.WebhookURL, "下单出错，方向：sell-limit，请登录服务器查看日志")
						continue
					} else {
						isMakeSellOrder = true
					}
				}

			}

			// 挂买单
			if o.Limittype == "sell-limit" && !isMakeBuyOrder {
				spew.Dump("挂买单", o.Limittype)
				// 最近一个是卖单成交，则开启一个买单，
				// 买入价格：最近成交价 * (100-1.2*rate)/100
				// 买入数量:AmountPerTrade/买入价格
				firstBuyPriceD := mul(decimal.NewFromFloat(o.Price),
					decimal.NewFromFloat((100-1.2*config.Rate)/100*math.Pow(10, config.PriceAccuracy)))
				price := float64(getInt(firstBuyPriceD)) / math.Pow(10, config.PriceAccuracy)
				amountD := decimal.NewFromFloat(config.AmountPerTrade / price *
					math.Pow(10, config.AmountAccuracy))
				amount := float64(getInt(amountD)) / math.Pow(10, config.AmountAccuracy)

				if price > config.MaxPrice || price < config.MinPrice {
					utils.Notify(config.WebhookURL,
						fmt.Sprintf("下单出错，方向：buy-limit，市价异常,%s 市价: %s usdt", config.Pair, strconv.FormatFloat(price, 'f', -1, 64)))
					return
				} else {
					_, err := makeOrder(AccountId, "buy-limit",
						strconv.FormatFloat(price, 'f', -1, 64),
						strconv.FormatFloat(amount, 'f', -1, 64))
					if err != nil {
						utils.Error(err, "下单出错")
						utils.Notify(config.WebhookURL, "下单出错，方向：buy-limit，请登录服务器查看日志")
						return
					} else {
						isMakeBuyOrder = true
					}

				}

			}

		}

	}
}
