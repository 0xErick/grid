package utils

import (
	"io/ioutil"

	"github.com/BurntSushi/toml"
	_ "github.com/mattn/go-sqlite3"
)

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

func GetConfig() (Config, error) {
	// 读取配置文件
	f, err := ioutil.ReadFile("./config.toml")
	if err != nil {
		Error(err, "读取配置文件出错：")
		return Config{}, err
	}

	// 解析配置文件
	var config Config
	if _, err := toml.Decode(string(f), &config); err != nil {
		Error(err, "解析配置文件出错：")
		return Config{}, err
	}

	return config, nil
}
