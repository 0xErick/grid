# 网格交易机器人（基于火币交易所）

## 使用

- 下载二进制程序

```
https://github.com/saqing/grid/releases/tag/v0.2
```

- 进行配置

```toml
#### config.toml
AccessKey="火币API AK"  
SecretKey="火币API SK"  
Host="api-aws.huobi.pro"
Pair="交易对比如  dotusdt" 
AmountPerTrade=250.0 // 每次下单金额
MaxOpenOrders=4  //最大开放订单数量
Rate=10.0       // 比例
MaxPrice=25.0   // 下单最大价格
MinPrice=5.0    // 下单最小价格
DBPath="./grid.db" // sqlite 数据库路径
WebhookURL="https://oapi.dingtalk.com/robot/send?access_token=xxx"  // DINGDING 通知地址
PriceAccuracy=4.0  // 交易对价格小数点位数
AmountAccuracy=4.0  // 交易对数量小数点位数
```

- 运行程序

```
nohup ./grid > error.log 2>&1
```

### QQ交流 1524915647
