package main

import (
	"context"
	"gift-bot"
	"gift-bot/internal/handler"
	"gift-bot/internal/repository"
	"gift-bot/internal/service"
	"gift-bot/pkg/config"
	"gift-bot/pkg/postgres"
	"github.com/gin-gonic/gin"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	os.Setenv("TZ", "Europe/Moscow")
	config.GlobalСonfig.Init()

	db, err := postgres.New()
	if err != nil {
		log.Fatalf("can't create new postgres db: %s", err.Error())
	}
	postgres.MigrateDB(db, config.GlobalСonfig.DB.Name)

	repos := repository.NewRepositories(db)
	services := service.NewServices(repos)
	handlers := handler.NewHandlers(services)

	// UTC+3
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		log.Fatalf("Failed to load location: %v", err)
	}

	log.Println("Timezone set to Europe/Moscow")

	go services.TelegramService.Start()

	services.TelegramService.NotifyUpcomingBirthdays()

	// Запуск горутины для периодического выполнения задачи
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

	//s := gocron.NewScheduler(time.UTC)
	//s.Every(1).Day().At("8:10").Do(services.TelegramService.NotifyUpcomingBirthdays)

	gin.SetMode(config.GlobalСonfig.ServerConfig.GinMode)
	srv := new(wifi.Server)
	if err := srv.Run(config.GlobalСonfig.ServerConfig.Port, handlers.InitRoutes()); err != nil {
		log.Fatalf("Error occurred while running http server, %s", err.Error())
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	log.Print("GiftBot project Shutting Down")

	if err := srv.Shutdown(context.Background()); err != nil {
		log.Fatalf("error occured on server shutting down: %s", err.Error())
	}

	if err := db.Close(); err != nil {
		log.Fatalf("error occured on db connection close: %s", err.Error())
	}

}
