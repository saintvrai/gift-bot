package config

import (
	"fmt"
	"github.com/spf13/viper"
	"log"
)

type Config struct {
	DB           PostgresConfig `mapstructure:"db"`
	ServerConfig ServerConfig   `mapstructure:"server"`
	Telegram     TelegramConfig `mapstructure:"telegram"`
}

type PostgresConfig struct {
	Host       string `mapstructure:"host"`
	Port       string `mapstructure:"port"`
	Username   string `mapstructure:"username"`
	Name       string `mapstructure:"name"`
	Password   string `mapstructure:"password"`
	SSL        string `mapstructure:"sslmode"`
	Migrations string `mapstructure:"migrations"`
}

type ServerConfig struct {
	Port     string `mapstructure:"port"`
	GinMode  string `mapstructure:"ginmode"`
	Timezone string `mapstructure:"timezone"`
}

type TelegramConfig struct {
	Token  string `mapstructure:"token"`
	Host   string `mapstructure:"host"`
	Secret string `mapstructure:"secret"`
}

var GlobalСonfig Config

// Init Метод чтения данных из config
func (c *Config) Init() {
	viper.SetConfigFile("./configs/config.yml")
	if err := viper.ReadInConfig(); err != nil {
		fmt.Printf("can't read config: %+v\n", err)
		log.Fatalf("can't read config: %+v", err)
	}
	err := viper.Unmarshal(c)
	if err != nil {
		fmt.Printf("unable to decode config into struct, %v\n", err)
		log.Fatalf("unable to decode config into struct, %v", err)
	}
}
