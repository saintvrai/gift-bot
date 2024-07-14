package models

import "time"

type User struct {
	ID         int64
	TelegramID int64
	Username   string
	FirstName  string
	LastName   string
	Role       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
