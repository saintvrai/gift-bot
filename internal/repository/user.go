package repository

import (
	"gift-bot/pkg/models"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
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
	query := `INSERT INTO users (telegram_id, username, first_name, last_name, role, birthdate, created_at, updated_at)
              VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := u.db.Exec(query, user.TelegramID, user.Username, user.FirstName, user.LastName, user.Role, user.Birthdate, time.Now(), time.Now())
	if err != nil {
		log.Errorf("create user err: %v", err)
		return err
	}
	return nil
}

func (u UserRepositoryImpl) UpdateUser(user models.User) error {
	query := `UPDATE users SET username=$1, first_name=$2, last_name=$3, role=$4, birthdate=$5, updated_at=$6 WHERE telegram_id=$7`
	_, err := u.db.Exec(query, user.Username, user.FirstName, user.LastName, user.Role, user.Birthdate, time.Now(), user.TelegramID)
	if err != nil {
		log.Errorf("update user err: %v", err)
		return err
	}
	return nil
}

func (u UserRepositoryImpl) GetUsersWithBirthdayInDays() ([]models.User, error) {
	query := `
    SELECT id, telegram_id, username, first_name, last_name, role, birthdate, created_at, updated_at
    FROM users
    WHERE birthdate IS NOT NULL 
    AND (EXTRACT(DOY FROM birthdate) - EXTRACT(DOY FROM NOW())) = 3
    OR (EXTRACT(DOY FROM birthdate) - EXTRACT(DOY FROM NOW())) = -3;`

	rows, err := u.db.Query(query)
	if err != nil {
		log.Errorf("get users with birthday in 3 days err: %v", err)
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(&user.ID, &user.TelegramID, &user.Username, &user.FirstName, &user.LastName, &user.Role, &user.Birthdate, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			log.Errorf("scan user err: %v", err)
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

func (u UserRepositoryImpl) GetAllAdmins() ([]models.User, error) {
	query := `
    SELECT id, telegram_id, username, first_name, last_name, role, birthdate, created_at, updated_at
    FROM users
    WHERE role = 'admin'`
	rows, err := u.db.Query(query)
	if err != nil {
		log.Errorf("get all admins err: %v", err)
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(&user.ID, &user.TelegramID, &user.Username, &user.FirstName, &user.LastName, &user.Role, &user.Birthdate, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			log.Errorf("scan user err: %v", err)
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
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

func (u UserRepositoryImpl) GetAllUsers() ([]models.User, error) {
	query := `SELECT id, telegram_id, username, first_name, last_name, role, created_at, updated_at FROM users;`
	rows, err := u.db.Query(query)
	if err != nil {
		log.Errorf("get all users query err: %v", err)
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(&user.ID, &user.TelegramID, &user.Username, &user.FirstName, &user.LastName, &user.Role, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			log.Errorf("get all users scan err: %v", err)
			return nil, err
		}
		users = append(users, user)
	}

	if err = rows.Err(); err != nil {
		log.Errorf("get all users rows err: %v", err)
		return nil, err
	}

	return users, nil
}

func (u UserRepositoryImpl) DeleteUsersByUsernames(usernames []string) error {
	query := `DELETE FROM users WHERE username = ANY($1::text[]);`
	_, err := u.db.Exec(query, pq.Array(usernames))
	if err != nil {
		log.Errorf("delete users by usernames err: %v", err)
		return err
	}
	return nil
}
