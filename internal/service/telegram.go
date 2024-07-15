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
	Bot           *tgbotapi.BotAPI
	userService   UserService
	loginAttempts map[int64]int
	loginState    map[int64]bool
	blockedUsers  map[int64]time.Time
	messageState  map[int64]bool
}

func NewTelegramService(userService UserService) *Telegram {
	return &Telegram{
		userService: userService,
	}
}

var (
	secretWord = &config.GlobalСonfig.Telegram.Secret
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

	// Инициализируем мапы для отслеживания состояния пользователей
	t.loginAttempts = make(map[int64]int)
	t.loginState = make(map[int64]bool)
	t.blockedUsers = make(map[int64]time.Time)
	t.messageState = make(map[int64]bool)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID

		// Проверяем, заблокирован ли пользователь
		if blockTime, blocked := t.blockedUsers[chatID]; blocked {
			if time.Since(blockTime) < 24*time.Hour {
				msg := tgbotapi.NewMessage(chatID, "Вы заблокированы на 24 часа из-за превышения количества попыток ввода секретного слова.")
				bot.Send(msg)
				continue
			} else {
				// Убираем блокировку после 24 часов
				delete(t.blockedUsers, chatID)
			}
		}

		// Проверка состояния сообщения администратора
		if t.messageState[chatID] {
			t.messageState[chatID] = false
			users, err := t.userService.GetAllUsers()
			if err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при получении списка пользователей.")
				bot.Send(msg)
				continue
			}

			for _, user := range users {
				msg := tgbotapi.NewMessage(user.TelegramID, update.Message.Text)
				bot.Send(msg)
			}
			msg := tgbotapi.NewMessage(chatID, "Сообщение отправлено всем пользователям.")
			bot.Send(msg)
			continue
		}

		// Проверка состояния логина
		if t.loginState[chatID] {
			// Обрабатываем попытки ввода секретного слова
			if update.Message.Text == *secretWord {
				user := models.User{
					TelegramID: chatID,
					Username:   update.Message.Chat.UserName,
					FirstName:  update.Message.Chat.FirstName,
					LastName:   update.Message.Chat.LastName,
					Role:       "user",
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}

				err := t.userService.CreateUser(user)
				if err != nil {
					log.Println(err)
					continue
				}

				msg := tgbotapi.NewMessage(chatID, "Вы успешно зарегистрированы в боте!")
				bot.Send(msg)

				// Сбрасываем состояние после успешного логина
				t.loginState[chatID] = false
				t.loginAttempts[chatID] = 0
			} else {
				t.loginAttempts[chatID]++
				if t.loginAttempts[chatID] >= 3 {
					msg := tgbotapi.NewMessage(chatID, "Вы исчерпали количество попыток ввода секретного слова и заблокированы на 24 часа.")
					bot.Send(msg)
					t.loginState[chatID] = false
					t.loginAttempts[chatID] = 0
					t.blockedUsers[chatID] = time.Now()
				} else {
					msg := tgbotapi.NewMessage(chatID, "Неправильное секретное слово, попробуйте снова.")
					bot.Send(msg)
				}
			}
			continue
		}

		switch update.Message.Text {
		case "/start":
			user := models.User{
				TelegramID: chatID,
			}

			user.Username = update.Message.Chat.UserName
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Привет, %s! Это простой телеграм бот для поздравляшек "+
				"своих близких коллег. Тут есть пару команд, чтобы ты мог начать получать сообщения! Если что-то будет "+
				"не так, то ты всегда можешь написать своему администратору для устранения проблем", user.Username))
			msg.ParseMode = "Markdown"
			bot.Send(msg)

		case "/chat":
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Ваш уникальный номер чата: `%d`", chatID))
			msg.ParseMode = "Markdown"
			bot.Send(msg)

		case "/login":
			user := models.User{
				TelegramID: chatID,
			}

			existingUser, err := t.userService.GetUser(user)
			if err != nil && err != sql.ErrNoRows {
				log.Errorf("error getting existing user: %v", err)
				continue
			}

			if existingUser.TelegramID != 0 {
				msg := tgbotapi.NewMessage(chatID, "Вы уже зарегистрированы в боте.")
				bot.Send(msg)
				continue
			}

			msg := tgbotapi.NewMessage(chatID, "Напишите секретное слово, которое вам выдали, для регистрации в боте")
			msg.ParseMode = "Markdown"
			bot.Send(msg)

			// Устанавливаем состояние логина для пользователя
			t.loginState[chatID] = true
			t.loginAttempts[chatID] = 0

		case "/message":
			user, err := t.userService.GetUser(models.User{TelegramID: chatID})
			if err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
				bot.Send(msg)
				continue
			}

			if user.Role == "admin" {
				msg := tgbotapi.NewMessage(chatID, "Введите сообщение, которое хотите отправить всем пользователям:")
				bot.Send(msg)
				t.messageState[chatID] = true
			} else {
				msg := tgbotapi.NewMessage(chatID, "У вас нет прав для использования этой команды.")
				bot.Send(msg)
			}

		default:
			msg := tgbotapi.NewMessage(chatID, "К сожалению, я вас не понял.")
			msg.ParseMode = "Markdown"
			bot.Send(msg)
		}
	}
	return bot
}
