package repository

import (
	"gift-bot/pkg/models"
	"github.com/jmoiron/sqlx"
	log "github.com/sirupsen/logrus"
	"time"
)

type UserRepositoryImpl struct {
	db *sqlx.DB
}

func NewUserRepository(db *sqlx.DB) *UserRepositoryImpl {
	return &UserRepositoryImpl{
		db: db,
	}
}

func (u UserRepositoryImpl) CreateUser(user models.User) error {
	query := `
		INSERT INTO users (telegram_id, username, first_name, last_name, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (telegram_id) DO NOTHING;
	`
	_, err := u.db.Exec(query, user.TelegramID, user.Username, user.FirstName, user.LastName, user.Role, time.Now(), time.Now())
	if err != nil {
		log.Errorf("create user err: %v", err)
		return err
	}
	return nil
}

func (u UserRepositoryImpl) GetUser(user models.User) (models.User, error) {
	query := `SELECT id, telegram_id, username, first_name, last_name, role, created_at, updated_at FROM users WHERE telegram_id=$1;`
	row := u.db.QueryRow(query, user.TelegramID)

	var foundUser models.User
	err := row.Scan(&foundUser.ID, &foundUser.TelegramID, &foundUser.Username, &foundUser.FirstName, &foundUser.LastName, &foundUser.Role, &foundUser.CreatedAt, &foundUser.UpdatedAt)
	if err != nil {
		log.Errorf("get user err: %v", err)
		return models.User{}, err
	}
	return foundUser, nil
}
