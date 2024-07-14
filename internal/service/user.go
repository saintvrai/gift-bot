package service

import (
	"gift-bot/internal/repository"
	"gift-bot/pkg/models"
)

type UserServiceImpl struct {
	repo repository.UserRepository
}

func NewUserService(repo repository.UserRepository) *UserServiceImpl {
	return &UserServiceImpl{repo: repo}
}

func (u UserServiceImpl) CreateUser(user models.User) error {
	return u.repo.CreateUser(user)
}

func (u UserServiceImpl) GetUser(user models.User) (models.User, error) {
	return u.repo.GetUser(user)
}
