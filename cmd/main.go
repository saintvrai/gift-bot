package main

import (
	"context"
	"gift-bot"
	"gift-bot/internal/handler"
	"gift-bot/internal/repository"
	"gift-bot/internal/service"
	"gift-bot/pkg/config"
	"gift-bot/pkg/postgres"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	os.Setenv("TZ", "Europe/Moscow")
	config.GlobalСonfig.Init()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	retryCfg := postgres.RetryConfig{
		BaseDelay:   1 * time.Second,
		MaxDelay:    30 * time.Second,
		PingTimeout: 5 * time.Second,
		Jitter:      0.2,
	}

	dbManager, err := postgres.NewManager(ctx, retryCfg, log.Printf)
	if err != nil {
		log.Fatalf("can't create new postgres db: %s", err.Error())
	}
	postgres.MigrateDB(dbManager.DB(), config.GlobalСonfig.DB.Name)
	go dbManager.MonitorAndReconnect(ctx, 5*time.Second)

	repos := repository.NewRepositories(dbManager)
	services := service.NewServices(repos)
	handlers := handler.NewHandlers(services)

	// UTC+3
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		log.Fatalf("Failed to load location: %v", err)
	}

	log.Println("Timezone set to Europe/Moscow")

	go services.TelegramService.Start()

	// Запуск горутины для периодического выполнения проверки ближайших дней рождений
	go func() {
		for {
			now := time.Now().In(loc)
			nextRun := time.Date(now.Year(), now.Month(), now.Day(), 9, 00, 0, 0, loc)
			if now.After(nextRun) {
				nextRun = nextRun.Add(24 * time.Hour)
			}
			time.Sleep(time.Until(nextRun))

			log.Println("Running scheduled task")
			services.TelegramService.NotifyUpcomingBirthdays()
		}
	}()

	// Ежедневная синхронизация профилей пользователей (ник/имя/фамилия)
	go func() {
		for {
			now := time.Now().In(loc)
			nextRun := time.Date(now.Year(), now.Month(), now.Day(), 4, 0, 0, 0, loc)

			if now.After(nextRun) {
				nextRun = nextRun.Add(24 * time.Hour)
			}
			time.Sleep(time.Until(nextRun))

			log.Println("Running daily user profile sync")
			services.TelegramService.SyncUserProfiles()
		}
	}()

	gin.SetMode(config.GlobalСonfig.ServerConfig.GinMode)
	srv := new(wifi.Server)
	if err := srv.Run(config.GlobalСonfig.ServerConfig.Port, handlers.InitRoutes()); err != nil {
		log.Fatalf("Error occurred while running http server, %s", err.Error())
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit
	cancel()

	log.Print("GiftBot project Shutting Down")

	if err = srv.Shutdown(context.Background()); err != nil {
		log.Fatalf("error occured on server shutting down: %s", err.Error())
	}

	if err = dbManager.Close(); err != nil {
		log.Fatalf("error occured on db connection close: %s", err.Error())
	}

}
