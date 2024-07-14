package service

import (
	"database/sql"
	"fmt"
	"gift-bot/pkg/config"
	"gift-bot/pkg/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	log "github.com/sirupsen/logrus"
	"time"
)

type Telegram struct {
	Bot         *tgbotapi.BotAPI
	userService UserService
}

func NewTelegramService(userService UserService) *Telegram {
	return &Telegram{
		userService: userService,
	}
}

var backButton = tgbotapi.NewKeyboardButton("Назад")
var startButton = tgbotapi.NewReplyKeyboard()

var (
	secretWordEntered = make(map[int64]bool) // Карта для отслеживания ввода секретного слова по chatID
	secretWord        = config.GlobalСonfig.Telegram.Secret
)

func (t *Telegram) Start() *tgbotapi.BotAPI {
	bot, err := tgbotapi.NewBotAPI(config.GlobalСonfig.Telegram.Token)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = false
	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil { // ignore any non-Message updates
			continue
		}

		chatID := update.Message.Chat.ID

		switch update.Message.Text {
		case "/chat":
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Ваш уникальный номер чата: `%d`", chatID))
			msg.ParseMode = "Markdown"
			bot.Send(msg)

		case "/login":
			msg := tgbotapi.NewMessage(chatID, "Напишите секретное слово, которое вам выдали, для регистрации в боте")
			msg.ParseMode = "Markdown"
			bot.Send(msg)
			secretWordEntered[chatID] = true

		default:
			// Проверяем, ожидает ли пользователь ввода секретного слова
			if secretWordEntered[chatID] {
				if update.Message.Text == secretWord {
					user := models.User{
						TelegramID: chatID,
						Username:   update.Message.Chat.UserName,
						FirstName:  update.Message.Chat.FirstName,
						LastName:   update.Message.Chat.LastName,
						Role:       "user",
						CreatedAt:  time.Now(),
						UpdatedAt:  time.Now(),
					}

					existingUser, err := t.userService.GetUser(user)
					if err != nil && err != sql.ErrNoRows {
						log.Errorf("error getting existing user: %v", err)
						continue
					}

					if existingUser.TelegramID != 0 {
						msg := tgbotapi.NewMessage(chatID, "Вы уже зарегистрированы в боте.")
						bot.Send(msg)
						delete(secretWordEntered, chatID) // Удаляем из карты, т.к. регистрация завершена
						continue
					}

					err = t.userService.CreateUser(user)
					if err != nil {
						log.Println(err)
						continue
					}

					msg := tgbotapi.NewMessage(chatID, "Вы успешно зарегистрированы в боте!")
					bot.Send(msg)
					delete(secretWordEntered, chatID) // Удаляем из карты, т.к. регистрация завершена
				} else {
					msg := tgbotapi.NewMessage(chatID, "Неправильное секретное слово, попробуйте снова.")
					bot.Send(msg)
				}
			}
		}
	}
	return bot
}
