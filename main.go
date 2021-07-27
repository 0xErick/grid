package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/davecgh/go-spew/spew"
	_ "github.com/mattn/go-sqlite3"
	"github.com/shopspring/decimal"

	log "github.com/go-ozzo/ozzo-log"
	"github.com/huobirdcenter/huobi_golang/pkg/client"
	"github.com/huobirdcenter/huobi_golang/pkg/model"
	"github.com/huobirdcenter/huobi_golang/pkg/model/order"
	"github.com/royeo/dingrobot"
)

// import "github.com/syndtr/goleveldb/leveldb"

type Config struct {
	AccessKey      string
	SecretKey      string
	Host           string
	Pair           string
	Accountid      string
	DBPath         string
	WebhookURL     string
	Rate           float64
	PriceAccuracy  float64
	AmountAccuracy float64
	AmountPerTrade float64
	MaxPrice       float64
	MinPrice       float64
}

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

func main() {

	// 控制台日志
	console := log.NewLogger()
	t1 := log.NewConsoleTarget()
	console.Targets = append(console.Targets, t1)

	console.Open()
	defer console.Close()

	//文件日志
	logger := log.NewLogger()
	t2 := log.NewFileTarget()
	t2.FileName = "logs/app.log"
	t2.MaxLevel = log.LevelError

	logger.Targets = append(logger.Targets, t2)

	logger.Open()
	defer logger.Close()

	// 读取配置文件
	f, err := ioutil.ReadFile("./config.toml")
	if err != nil {
		logger.Error("读取配置文件出错：", err)
		return
	}

	// 解析配置文件
	var config Config
	if _, err := toml.Decode(string(f), &config); err != nil {
		logger.Error("解析配置文件出错：", err)
		return
	}

	// 读取账户ID
	AccountId, err := getAccounId(config.AccessKey, config.SecretKey, config.Host)

	if err != nil {
		logger.Error("读取账户ID出错", err)
		return
	}

	console.Info("账户ID：", AccountId)

	// // 下单 计算值  下单方向  价格  数量
	// res2, err := makeOrder(config.AccessKey, config.SecretKey, config.Host, AccountId, "buy-limit", config.Pair, "7", "1")
	// if err != nil {
	// 	logger.Error("下单出错", err)
	// 	dingdingNotify(config.WebhookURL, "下单出错，方向：，请登录服务器查看日志") //等待完善
	// 	return
	// }

	// console.Info("调用结果", res2)

	timeTickerChan := time.Tick(time.Second * 1)
	for {

		var cstZone = time.FixedZone("CST", 8*3600) // 东八
		t4 := time.Now().In(cstZone).Hour()         //小时
		t5 := time.Now().Minute()                   //分钟
		t6 := time.Now().Second()                   //秒

		// 北京时间中午 11点半 准时 发送一次 当前资产估值
		if t4 == 11 && t5 == 30 && t6 == 0 {
			getCurrentMoney(config.AccessKey, config.SecretKey, config.Host, AccountId, config.Pair, config.WebhookURL)
		}

		unix := time.Now().Unix()

		// 10秒进行一次计算
		if unix%5 == 0 {
			console.Info("时间戳：", unix)

			// SQLITE
			db, err := sql.Open("sqlite3", config.DBPath)
			checkErr(err, config.WebhookURL)
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
			db.Exec(sql_table) //执行数据表
			rows, err := db.Query("SELECT COUNT(*) as count FROM orders WHERE state='unfinished'")
			if err != nil {
				logger.Error("查询数量出错", err)
				continue
			}

			count := 0
			for rows.Next() {
				err := rows.Scan(&count)
				checkErr(err, config.WebhookURL)
			}

			console.Info("count unfinished:", count)
			rowsFinshed, err := db.Query("SELECT COUNT(*) as count FROM orders WHERE state='finished'")
			if err != nil {
				logger.Error("查询数量出错", err)
				continue
			}

			count2 := 0
			for rowsFinshed.Next() {
				err := rowsFinshed.Scan(&count2)
				checkErr(err, config.WebhookURL)
			}
			console.Info("count finished:", count2)

			// 读取火币上的开放订单
			openOrders, err := getOpenOrders(config.AccessKey, config.SecretKey, config.Host, AccountId, config.Pair)

			if err != nil {
				logger.Error("getOpenOrders", err)
				continue
			}

			// 如果开放订单数量为0 且sqlite 买卖平衡  则开启一个买入单
			if len(openOrders) == 0 && count2 == count {
				latestPrice, err := getLatestTrade(config.Host, config.Pair)
				if err != nil {
					logger.Error("读取最近成交价格出错", err)
					continue
				}

				// 第一次买入
				// 买入价格：市价 * (100-rate)/100 买入数量：AmountPerTrade/买入价格
				firstBuyPriceD := mul(latestPrice, decimal.NewFromFloat((100-config.Rate)/100*math.Pow(10, config.PriceAccuracy)))
				price := float64(getInt(firstBuyPriceD)) / math.Pow(10, config.PriceAccuracy)
				amountD := decimal.NewFromFloat(config.AmountPerTrade / price * math.Pow(10, config.AmountAccuracy))
				amount := float64(getInt(amountD)) / math.Pow(10, config.AmountAccuracy)

				if price > config.MaxPrice || price < config.MinPrice {
					dingdingNotify(config.WebhookURL, fmt.Sprintf("下单出错，方向：buy-limit，市价异常,%s 市价: %s usdt", config.Pair, strconv.FormatFloat(price, 'f', -1, 64)))
				} else {
					// spew.Dump(strconv.FormatFloat(price, 'f', -1, 64), strconv.FormatFloat(amount, 'f', -1, 64))
					_, err := makeOrder(config.AccessKey, config.SecretKey, config.Host, AccountId, "buy-limit", config.Pair, strconv.FormatFloat(price, 'f', -1, 64), strconv.FormatFloat(amount, 'f', -1, 64), db, config.WebhookURL)
					if err != nil {
						logger.Error("下单出错", err)
						dingdingNotify(config.WebhookURL, "下单出错，方向：buy-limit，请登录服务器查看日志")

					}
				}
			}

			// 补仓买入
			// 当前价格是否低于开放订单最低价格的  且开放订单数量少于4个 补仓买入
			if len(openOrders) > 0 && len(openOrders) < 4 {
				latestPrice, err := getLatestTrade(config.Host, config.Pair)
				minPriceInOpenOrder := 0.0
				if err != nil {
					logger.Error("读取最近成交价格出错", err)
				} else {
					// 买入价格：开放卖出订单中的最低价格 * (100-rate-rate) /100  买入数量:AmountPerTrade/买入价格

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
						targetPriceD := mul(decimal.NewFromFloat(minPriceInOpenOrder), decimal.NewFromFloat((100-config.Rate-config.Rate)/100*math.Pow(10, config.PriceAccuracy)))
						targetPrice := float64(getInt(targetPriceD)) / math.Pow(10, config.PriceAccuracy)

						amountD := decimal.NewFromFloat(config.AmountPerTrade / targetPrice * math.Pow(10, config.AmountAccuracy))
						amount := float64(getInt(amountD)) / math.Pow(10, config.AmountAccuracy)
						if getFloat(latestPrice) < targetPrice {
							if targetPrice > config.MaxPrice || targetPrice < config.MinPrice {
								dingdingNotify(config.WebhookURL, fmt.Sprintf("下单出错，方向：buy-limit，市价异常,%s 市价: %s usdt", config.Pair, strconv.FormatFloat(targetPrice, 'f', -1, 64)))

							} else {
								_, err := makeOrder(config.AccessKey, config.SecretKey, config.Host, AccountId, "buy-limit", config.Pair, strconv.FormatFloat(targetPrice, 'f', -1, 64), strconv.FormatFloat(amount, 'f', -1, 64), db, config.WebhookURL)
								if err != nil {
									logger.Error("下单出错", err)
									dingdingNotify(config.WebhookURL, "下单出错，方向：buy-limit，请登录服务器查看日志")
								}
							}

						}
					}

				}
			}

			// 成交事件
			// 读取 sqlite3 中的下单数据
			order_rows, err := db.Query("SELECT * FROM orders where state=='unfinished'")
			if err != nil {
				logger.Error("sqlite rows 读取出错", err)
				continue
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
				checkErr(err, config.WebhookURL)
				orders = append(orders, Order{Id: id, Orderid: orderid, Symbol: symbol, Price: price, Amount: amount, State: state, Limittype: limittype, Createdat: createdat})

			}
			isMakeSellOrder := false
			isMakeBuyOrder := false
			for _, o := range orders {
				isFullfill := true
				for _, oo := range openOrders {
					spew.Dump("订单号是否相同", strconv.Itoa(int(oo.Id)), o.Orderid)

					if strconv.Itoa(int(oo.Id)) == o.Orderid {
						isFullfill = false
					}

				}

				if isFullfill {
					spew.Dump("订单完成,进入更新", o.Orderid)
					stmt, err := db.Prepare("update orders set state=? where orderid=?")
					if err != nil {
						logger.Error("更新订单状态 prepare", err)
						continue
					}

					_, err = stmt.Exec("finished", o.Orderid)
					if err != nil {
						logger.Error("更新订单状态 exec", err)
						continue
					}

					spew.Dump("准备挂单", o.Orderid, o.Limittype, isMakeBuyOrder, isMakeSellOrder)

					// 挂卖单
					if o.Limittype == "buy-limit" && !isMakeSellOrder {
						spew.Dump("挂卖单", o.Limittype)
						// 最近一个是买单成交，则开启一个卖单，卖出价格：最近成交价 * (100+rate)/100  卖出数量 = 最近成交的买入数量
						firstBuyPriceD := mul(decimal.NewFromFloat(o.Price), decimal.NewFromFloat((100+config.Rate)/100*math.Pow(10, config.PriceAccuracy)))
						price := float64(getInt(firstBuyPriceD)) / math.Pow(10, config.PriceAccuracy)
						amountD := decimal.NewFromFloat(o.Amount * 0.995 * math.Pow(10, config.AmountAccuracy))
						amount := float64(getInt(amountD)) / math.Pow(10, config.AmountAccuracy)

						if price > config.MaxPrice || price < config.MinPrice {
							dingdingNotify(config.WebhookURL, fmt.Sprintf("下单出错，方向：sell-limit，市价异常,%s 市价: %s usdt", config.Pair, strconv.FormatFloat(price, 'f', -1, 64)))
							continue
						} else {
							// spew.Dump(strconv.FormatFloat(price, 'f', -1, 64), strconv.FormatFloat(amount, 'f', -1, 64))
							_, err := makeOrder(config.AccessKey, config.SecretKey, config.Host, AccountId, "sell-limit", config.Pair, strconv.FormatFloat(price, 'f', -1, 64), strconv.FormatFloat(amount, 'f', -1, 64), db, config.WebhookURL)
							if err != nil {
								logger.Error("下单出错", err)
								dingdingNotify(config.WebhookURL, "下单出错，方向：sell-limit，请登录服务器查看日志")
								continue
							} else {
								isMakeSellOrder = true
							}

						}

					}

					// 挂买单
					if o.Limittype == "sell-limit" && !isMakeBuyOrder {
						spew.Dump("挂买单", o.Limittype)
						// 最近一个是卖单成交，则开启一个买单， 买入价格：最近成交价 * (100-1.2*rate)/100  买入数量:AmountPerTrade/买入价格
						firstBuyPriceD := mul(decimal.NewFromFloat(o.Price), decimal.NewFromFloat((100-1.2*config.Rate)/100*math.Pow(10, config.PriceAccuracy)))
						price := float64(getInt(firstBuyPriceD)) / math.Pow(10, config.PriceAccuracy)
						amountD := decimal.NewFromFloat(config.AmountPerTrade / price * math.Pow(10, config.AmountAccuracy))
						amount := float64(getInt(amountD)) / math.Pow(10, config.AmountAccuracy)

						if price > config.MaxPrice || price < config.MinPrice {
							dingdingNotify(config.WebhookURL, fmt.Sprintf("下单出错，方向：buy-limit，市价异常,%s 市价: %s usdt", config.Pair, strconv.FormatFloat(price, 'f', -1, 64)))
							continue
						} else {
							// spew.Dump(strconv.FormatFloat(price, 'f', -1, 64), strconv.FormatFloat(amount, 'f', -1, 64))
							_, err := makeOrder(config.AccessKey, config.SecretKey, config.Host, AccountId, "buy-limit", config.Pair, strconv.FormatFloat(price, 'f', -1, 64), strconv.FormatFloat(amount, 'f', -1, 64), db, config.WebhookURL)
							if err != nil {
								logger.Error("下单出错", err)
								dingdingNotify(config.WebhookURL, "下单出错，方向：buy-limit，请登录服务器查看日志")
								continue
							} else {
								isMakeBuyOrder = true
							}

						}

					}

				}

			}

			// spew.Dump(orders)
			console.Info("count openOrders:", len(openOrders))

		}

		<-timeTickerChan
	}

}

func getAccounId(AccessKey string, SecretKey string, Host string) (string, error) {
	client := new(client.AccountClient).Init(AccessKey, SecretKey, Host)
	resp, err := client.GetAccountInfo()
	if err != nil {
		return "", err
	}

	var accountId = ""

	for _, result := range resp {
		if result.Type == "spot" && result.State == "working" {
			accountId = strconv.Itoa(int(result.Id))
		}
	}
	if accountId == "" {
		return "", errors.New("未找到 AccountId")
	}
	return accountId, err
}

func makeOrder(AccessKey string, SecretKey string, Host string, AccountId string,
	Type string, Symbol string, Price string, Amount string, Db *sql.DB, WebhookURL string) (string, error) {
	client := new(client.OrderClient).Init(AccessKey, SecretKey, Host)
	request := order.PlaceOrderRequest{
		AccountId: AccountId,
		Type:      Type, //"buy-limit", "sell-limit",
		Source:    "spot-api",
		Symbol:    Symbol,
		Price:     Price,
		Amount:    Amount,
	}
	resp, err := client.PlaceOrder(&request)
	if err != nil {
		return "", err
	} else {
		switch resp.Status {
		case "ok":
			// 这里可能有bug
			stmt, err := Db.Prepare("INSERT INTO orders(orderid, symbol,price,amount,limittype,state,createdat)  values(?, ?,?,?,?,?,?)")
			if err != nil {
				dingdingNotify(WebhookURL, fmt.Sprintf("stmt 构造出错，orderid: %s", resp.Data))
			} else {
				_, err := stmt.Exec(resp.Data, Symbol, Price, Amount, Type, "unfinished", time.Now().Unix())
				if err != nil {
					dingdingNotify(WebhookURL, fmt.Sprintf("stmt 构造出错，orderid: %s", resp.Data))
				}
			}
			return resp.Data, nil
		case "error":
			return "", errors.New(resp.ErrorMessage)
		}

	}
	return "", errors.New("未知错误")
}

func dingdingNotify(WebhookURL string, message string) {

	logger := log.NewLogger()
	t2 := log.NewFileTarget()
	t2.FileName = "logs/dingding.log"
	t2.MaxLevel = log.LevelError
	logger.Targets = append(logger.Targets, t2)

	logger.Open()
	defer logger.Close()

	webhook := WebhookURL
	robot := dingrobot.NewRobot(webhook)

	content := message
	atMobiles := []string{}
	isAtAll := false
	err := robot.SendText(content, atMobiles, isAtAll)
	if err != nil {
		logger.Error("dingding 通知", err)
	}

}

func getAccountAssetValuation(AccessKey string, SecretKey string, Host string) (string, error) {
	client := new(client.AccountClient).Init(AccessKey, SecretKey, Host)
	resp, err := client.GetAccountAssetValuation("spot", "USD", 0)
	if err != nil {
		spew.Dump("Get account asset valuation error: %s", err)
		return "", err
	} else {
		return resp.Data.Balance, err
	}
}

func getCurrentMoney(AccessKey string, SecretKey string, Host string, AccountId string, Pair string, WebhookURL string) {
	logger := log.NewLogger()
	t2 := log.NewFileTarget()
	t2.FileName = "logs/getCurrentMoney.log"
	t2.MaxLevel = log.LevelError
	logger.Targets = append(logger.Targets, t2)

	logger.Open()
	defer logger.Close()

	client := new(client.AccountClient).Init(AccessKey, SecretKey, Host)
	resp, err := client.GetAccountBalance(AccountId)
	countSplit := strings.Split(Pair, "usdt")
	coin := countSplit[0]

	coinTotal := 0.0
	usdtTotal := 0.0
	coinFrozen := 0.0
	usdtFrozen := 0.0
	if err != nil {
		logger.Error("Get account error:", err)
	} else {
		spew.Dump(resp.Id, resp.Type, resp.State, len(resp.List))

		if resp.List != nil {
			for _, result := range resp.List {
				if result.Currency == coin {
					if result.Type == "frozen" {
						f, _ := strconv.ParseFloat(result.Balance, 64)
						coinFrozen = f
					}

					if result.Type == "trade" {
						f, _ := strconv.ParseFloat(result.Balance, 64)
						coinTotal = f

					}
				}
				if result.Currency == "usdt" {
					if result.Type == "frozen" {
						f, _ := strconv.ParseFloat(result.Balance, 64)
						usdtFrozen = f
						// dingdingNotify(WebhookURL, fmt.Sprintf("%s 冻结数量: %s 个", "usdt", result.Balance))
					}

					if result.Type == "trade" {
						f, _ := strconv.ParseFloat(result.Balance, 64)
						usdtTotal = f
					}
				}
			}
		}
	}

	balance, _ := getAccountAssetValuation(AccessKey, SecretKey, Host)

	dingdingNotify(WebhookURL, fmt.Sprintf("账户资产估值: %s USD\n%s 冻结数量: %v 个\n%s 可用数量: %v 个\n%s 冻结数量: %v 个\n%s 可用数量: %v 个", balance, coin, coinFrozen, coin, coinTotal, "usdt", usdtFrozen, "usdt", usdtTotal))
}

func getOpenOrders(AccessKey string, SecretKey string, Host string, AccountId string, Pair string) ([]order.OpenOrder, error) {
	client := new(client.OrderClient).Init(AccessKey, SecretKey, Host)
	request := new(model.GetRequest).Init()
	request.AddParam("account-id", AccountId)
	request.AddParam("symbol", Pair)
	resp, err := client.GetOpenOrders(request)
	if err != nil {
		return nil, err

	} else {
		switch resp.Status {
		case "ok":
			if resp.Data != nil {
				return resp.Data, nil

			}
		case "error":
			return nil, errors.New(resp.ErrorMessage)
		}
	}
	return nil, errors.New("未知错误")
}

func getLatestTrade(Host string, Pair string) (decimal.Decimal, error) {
	client := new(client.MarketClient).Init(Host)

	resp, err := client.GetLatestTrade(Pair)

	blank, _ := decimal.NewFromString("0")
	latestPrice, _ := decimal.NewFromString("0")
	if err != nil {
		return blank, err
	} else {
		for _, trade := range resp.Data {

			// spew.Dump("id price", trade.Id, trade.Price)
			latestPrice = trade.Price

		}
		return latestPrice, nil
	}
}

// 加法
func add(d1 decimal.Decimal, d2 decimal.Decimal) decimal.Decimal {
	return d1.Add(d2)
}

// 减法
func sub(d1 decimal.Decimal, d2 decimal.Decimal) decimal.Decimal {
	return d1.Sub(d2)
}

// 乘法
func mul(d1 decimal.Decimal, d2 decimal.Decimal) decimal.Decimal {
	return d1.Mul(d2)
}

// 除法
func div(d1 decimal.Decimal, d2 decimal.Decimal) decimal.Decimal {
	return d1.Div(d2)
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

// func ReadALl(db) {

// }

func checkErr(err error, WebhookURL string) {

	logger := log.NewLogger()
	t2 := log.NewFileTarget()
	t2.FileName = "logs/fatal.log"
	t2.MaxLevel = log.LevelError
	logger.Targets = append(logger.Targets, t2)

	logger.Open()
	defer logger.Close()
	if err != nil {
		logger.Error("致命错误", err)
		dingdingNotify(WebhookURL, "致命错误 出错")
		panic(err)
	}
}

func checkErr2(err error) {

	logger := log.NewLogger()
	t2 := log.NewFileTarget()
	t2.FileName = "logs/fatal.log"
	t2.MaxLevel = log.LevelError
	logger.Targets = append(logger.Targets, t2)

	logger.Open()
	defer logger.Close()
	if err != nil {
		logger.Error("致命错误", err)

	}
}

// https://github.com/HuobiRDCenter/huobi_Golang/blob/master/cmd/accountclientexample/accountclientexample.go
// 账户 订单 市场
