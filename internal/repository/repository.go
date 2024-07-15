package repository

import (
	"gift-bot/pkg/models"
	"github.com/jmoiron/sqlx"
)

type Repositories struct {
	UserRepository
}

func NewRepositories(db *sqlx.DB) *Repositories {
	userRepository := NewUserRepository(db)
	return &Repositories{UserRepository: userRepository}
}

type UserRepository interface {
	CreateUser(user models.User) error
	GetUser(user models.User) (models.User, error)
	GetAllUsers() ([]models.User, error)
}
