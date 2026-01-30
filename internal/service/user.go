package service

import (
	"gift-bot/internal/repository"
	"gift-bot/pkg/models"
	"time"
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

func (u UserServiceImpl) GetAllUsers() ([]models.User, error) {
	return u.repo.GetAllUsers()
}

func (u UserServiceImpl) GetBlockedUsers() ([]models.User, error) {
	return u.repo.GetBlockedUsers()
}

func (u UserServiceImpl) DeleteUsersByUsernames(usernames []string) error {
	return u.repo.DeleteUsersByUsernames(usernames)
}

func (u UserServiceImpl) UnblockUsersByUsernames(usernames []string) error {
	return u.repo.UnblockUsersByUsernames(usernames)
}

func (u UserServiceImpl) UpdateUser(user models.User) error {
	return u.repo.UpdateUser(user)
}

func (u UserServiceImpl) GetUsersWithBirthdayInDays() ([]models.User, error) {
	return u.repo.GetUsersWithBirthdayInDays()
}

func (u UserServiceImpl) GetAllAdmins() ([]models.User, error) {
	return u.repo.GetAllAdmins()
}

func (u UserServiceImpl) HasBirthdayNotification(adminTelegramID int64, userTelegramID int64, date time.Time) (bool, error) {
	return u.repo.HasBirthdayNotification(adminTelegramID, userTelegramID, date)
}

func (u UserServiceImpl) SaveBirthdayNotification(adminTelegramID int64, userTelegramID int64, date time.Time) error {
	return u.repo.SaveBirthdayNotification(adminTelegramID, userTelegramID, date)
}
