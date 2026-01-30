package repository

import (
	"gift-bot/pkg/models"
	"github.com/jmoiron/sqlx"
	"time"
)

type Repositories struct {
	UserRepository
}

type DBProvider interface {
	DB() *sqlx.DB
}

func NewRepositories(dbProvider DBProvider) *Repositories {
	userRepository := NewUserRepository(dbProvider)
	return &Repositories{UserRepository: userRepository}
}

type UserRepository interface {
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
