package service

import (
	"gift-bot/internal/repository"
	"gift-bot/pkg/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"time"
)

type Services struct {
	UserService
	TelegramService
}

func NewServices(repos *repository.Repositories) *Services {
	userService := NewUserService(repos.UserRepository)
	telegramService := NewTelegramService(userService)
	return &Services{
		UserService:     userService,
		TelegramService: telegramService,
	}
}

type UserService interface {
	CreateUser(user models.User) error
	GetUser(user models.User) (models.User, error)
	GetAllUsers() ([]models.User, error)
	GetBlockedUsers() ([]models.User, error)
	DeleteUsersByUsernames(usernames []string) error
	UnblockUsersByUsernames(usernames []string) error
	UpdateUser(user models.User) error
	GetUsersWithBirthdayInDays() ([]models.User, error)
	GetAllAdmins() ([]models.User, error)
	HasBirthdayNotification(adminTelegramID int64, userTelegramID int64, date time.Time) (bool, error)
	SaveBirthdayNotification(adminTelegramID int64, userTelegramID int64, date time.Time) error
}
type TelegramService interface {
	Start() *tgbotapi.BotAPI
	NotifyUpcomingBirthdays()
	SyncUserProfiles()
}
