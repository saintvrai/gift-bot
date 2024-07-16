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
)

func main() {
	config.GlobalСonfig.Init()

	db, err := postgres.New()
	if err != nil {
		log.Fatalf("can't create new postgres db2: %s", err.Error())
	}
	postgres.MigrateDB(db, config.GlobalСonfig.DB.Name)

	repos := repository.NewRepositories(db)
	services := service.NewServices(repos)
	handlers := handler.NewHandlers(services)
	services.TelegramService.Start()
	// Периодическое выполнение задачи
	services.TelegramService.NotifyUpcomingBirthdays()
	//s := gocron.NewScheduler(time.UTC)
	//s.Every(1).Day().At("09:00").Do(services.TelegramService.NotifyUpcomingBirthdays) // Установите время в 09:00 UTC

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
