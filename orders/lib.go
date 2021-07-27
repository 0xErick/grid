package orders

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/davecgh/go-spew/spew"
	_ "github.com/mattn/go-sqlite3"
	"github.com/shopspring/decimal"

	"grid/utils"

	"github.com/huobirdcenter/huobi_golang/pkg/client"
	"github.com/huobirdcenter/huobi_golang/pkg/model"
	"github.com/huobirdcenter/huobi_golang/pkg/model/order"
)

// import "github.com/syndtr/goleveldb/leveldb"

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

func GetAccounId(AccessKey string, SecretKey string, Host string) (string, error) {
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

func GetCurrentMoney(AccessKey string, SecretKey string, Host string, AccountId string, Pair string, WebhookURL string) {

	client := new(client.AccountClient).Init(AccessKey, SecretKey, Host)
	resp, err := client.GetAccountBalance(AccountId)
	countSplit := strings.Split(Pair, "usdt")
	coin := countSplit[0]

	coinTotal := 0.0
	usdtTotal := 0.0
	coinFrozen := 0.0
	usdtFrozen := 0.0
	if err != nil {
		utils.Error(err, "Get account error:")
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

	utils.Notify(WebhookURL, fmt.Sprintf("账户资产估值: %s USD\n%s 冻结数量: %v 个\n%s 可用数量: %v 个\n%s 冻结数量: %v 个\n%s 可用数量: %v 个", balance, coin, coinFrozen, coin, coinTotal, "usdt", usdtFrozen, "usdt", usdtTotal))
}

func CancelOrderById(AccessKey string, SecretKey string, Host string, Id string) (bool, error) {
	client := new(client.OrderClient).Init(AccessKey, SecretKey, Host)
	resp, err := client.CancelOrderById(Id)
	if err != nil {
		return false, err
	} else {
		switch resp.Status {
		case "ok":
			return true, nil
		case "error":
			return false, errors.New(resp.ErrorMessage)
		}
	}
	return true, nil
}

func GetOpenOrders(AccessKey string, SecretKey string, Host string, AccountId string, Pair string) ([]order.OpenOrder, error) {
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

func GetLatestTrade(Host string, Pair string) (decimal.Decimal, error) {
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

// https://github.com/HuobiRDCenter/huobi_Golang/blob/master/cmd/accountclientexample/accountclientexample.go
// 账户 订单 市场
