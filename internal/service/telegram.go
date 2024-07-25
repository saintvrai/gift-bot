package service

import (
	"database/sql"
	"fmt"
	"gift-bot/pkg/config"
	"gift-bot/pkg/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	log "github.com/sirupsen/logrus"
	"strings"
	"time"
)

type Telegram struct {
	Bot              *tgbotapi.BotAPI
	userService      UserService
	loginAttempts    map[int64]int
	loginState       map[int64]bool
	blockedUsers     map[int64]time.Time
	messageState     map[int64]string             // Состояние: "waiting_message" или "waiting_ignored_users"
	adminMessageData map[int64]*AdminMessageState // Состояние сообщения администратора

}

func NewTelegramService(userService UserService) *Telegram {
	bot, err := tgbotapi.NewBotAPI(config.GlobalСonfig.Telegram.Token)
	if err != nil {
		log.Panic(err)
	}

	return &Telegram{
		userService:      userService,
		Bot:              bot,
		loginAttempts:    make(map[int64]int),
		loginState:       make(map[int64]bool),
		blockedUsers:     make(map[int64]time.Time),
		messageState:     make(map[int64]string),
		adminMessageData: make(map[int64]*AdminMessageState),
	}
}

type AdminMessageState struct {
	Message     string
	IgnoredList []string
	User        models.User
}

var (
	secretWord = &config.GlobalСonfig.Telegram.Secret
)

// Добавьте новое состояние для ожидания даты рождения
const waitingBirthdateState = "waiting_birthdate"

func (t *Telegram) Start() *tgbotapi.BotAPI {
	bot := t.Bot

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil && update.CallbackQuery == nil {
			log.Println("1", update.Message.From.UserName, update.Message.Chat.ID)
			continue
		}

		var chatID int64
		var text string

		if update.Message != nil {
			chatID = update.Message.Chat.ID
			text = update.Message.Text
			log.Println("2", chatID, update.Message.Text, update.Message.From.UserName, update.Message.From.FirstName)
		} else if update.CallbackQuery != nil {
			chatID = update.CallbackQuery.Message.Chat.ID
			text = update.CallbackQuery.Data
			log.Println("3", chatID, update.Message.From.UserName, update.Message.Chat.ID)
		}

		if text == "/start" {

			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Привет! Это простой телеграм бот для поздравляшек "+
				"своих близких коллег. Тут есть пару команд, чтобы ты мог начать получать сообщения! Если что-то будет "+
				"не так, то ты всегда можешь написать своему администратору для устранения проблем"))
			msg.ParseMode = "Markdown"
			send, err := bot.Send(msg)
			if err != nil {
				log.Println("Error with start", send, err)
			}
			continue
		}

		// Проверяем, заблокирован ли пользователь
		existingUser, err := t.userService.GetUser(models.User{TelegramID: chatID})
		if err != nil && err != sql.ErrNoRows {
			log.Errorf("error getting existing user: %v", err)
			continue
		}

		if existingUser.Blocked {
			msg := tgbotapi.NewMessage(chatID, "Вы заблокированы.")
			bot.Send(msg)
			continue
		}

		// Обработка состояния администратора для отправки сообщений
		if state, exists := t.messageState[chatID]; exists {
			if text == "отмена" {
				delete(t.adminMessageData, chatID)
				t.messageState[chatID] = ""
				msg := tgbotapi.NewMessage(chatID, "Действие отменено. Введите /message для отправки нового сообщения.")
				bot.Send(msg)
				continue
			}

			switch state {
			case "waiting_message":
				log.Printf("Received message from admin: %s", text)
				t.adminMessageData[chatID] = &AdminMessageState{
					Message: text,
				}
				t.messageState[chatID] = "waiting_ignored_users"

				users, err := t.userService.GetAllUsers()
				if err != nil {
					log.Println(err)
					msg := tgbotapi.NewMessage(chatID, "Ошибка при получении списка пользователей.")
					bot.Send(msg)
					continue
				}

				keyboard := t.createUserSelectionKeyboard(users, true)
				msg := tgbotapi.NewMessage(chatID, "Выберите пользователей, которым не нужно отправлять сообщение. Для отмены введите 'отмена'.")
				msg.ReplyMarkup = keyboard
				bot.Send(msg)
				continue

			case "waiting_ignored_users":
				log.Printf("Received ignored users from admin: %s", text)
				if text == "отмена" {
					delete(t.adminMessageData, chatID)
					t.messageState[chatID] = ""
					msg := tgbotapi.NewMessage(chatID, "Действие отменено. Введите /message для отправки нового сообщения.")
					bot.Send(msg)
					continue
				}

				// Если это коллбэк от кнопки, добавляем пользователя в список игнорируемых
				if update.CallbackQuery != nil {
					if text == "send_message" {
						t.messageState[chatID] = ""
						t.sendMessageToUsers(chatID)
						continue
					}

					username := update.CallbackQuery.Data
					data := t.adminMessageData[chatID]
					data.IgnoredList = append(data.IgnoredList, username)

					// Обновляем клавиатуру, чтобы удалить выбранного пользователя
					users, err := t.userService.GetAllUsers()
					if err != nil {
						log.Println(err)
						msg := tgbotapi.NewMessage(chatID, "Ошибка при обновлении списка пользователей.")
						bot.Send(msg)
						continue
					}

					// Удаляем уже выбранных пользователей из клавиатуры
					var remainingUsers []models.User
					for _, user := range users {
						ignored := false
						for _, ignoredUser := range data.IgnoredList {
							if user.Username == ignoredUser {
								ignored = true
								break
							}
						}
						if !ignored {
							remainingUsers = append(remainingUsers, user)
						}
					}

					keyboard := t.createUserSelectionKeyboard(remainingUsers, true)
					editMsg := tgbotapi.NewEditMessageReplyMarkup(chatID, update.CallbackQuery.Message.MessageID, keyboard)
					bot.Send(editMsg)
					continue
				}

				// Если администратор завершил выбор
				if text == "нет" {
					t.messageState[chatID] = ""
					t.sendMessageToUsers(chatID)
					continue
				}

			case waitingBirthdateState:
				log.Printf("Received birthdate from user: %s", text)
				birthdate, err := time.Parse("02.01.2006", text)
				if err != nil {
					msg := tgbotapi.NewMessage(chatID, "Неверный формат даты. Пожалуйста, введите дату в формате ДД.ММ.ГГГГ:")
					bot.Send(msg)
					continue
				}

				user := t.adminMessageData[chatID].User
				user.Birthdate = birthdate
				err = t.userService.CreateUser(user)
				if err != nil {
					msg := tgbotapi.NewMessage(chatID, "Ошибка при создании пользователя.")
					bot.Send(msg)
					continue
				}

				delete(t.adminMessageData, chatID)
				t.messageState[chatID] = ""

				msg := tgbotapi.NewMessage(chatID, "Вы успешно зарегистрировались.")
				bot.Send(msg)
				// Отправляем админам уведомление о регистрации в боте пользователя

				admins, err := t.userService.GetAllAdmins()
				if err != nil {
					log.Println("Error getting all admins:", err)
					continue
				}

				for _, admin := range admins {
					message := fmt.Sprintf("Пользователь @%s зарегистрировался в боте", user.Username)
					msg := tgbotapi.NewMessage(admin.TelegramID, message)
					//fmt.Printf("Notifying admin %s about upcoming birthday of %s", admin.Username, birthdayUser.Username)
					t.Bot.Send(msg)
				}

				continue
			case "waiting_delete_users":
				if text == "отмена" {
					t.messageState[chatID] = ""
					msg := tgbotapi.NewMessage(chatID, "Действие отменено.")
					bot.Send(msg)
					continue
				}

				usernames := strings.Split(text, ",")
				for i := range usernames {
					usernames[i] = strings.TrimSpace(usernames[i])
				}

				err := t.userService.DeleteUsersByUsernames(usernames)
				if err != nil {
					log.Println(err)
					msg := tgbotapi.NewMessage(chatID, "Ошибка при удалении пользователей.")
					bot.Send(msg)
				} else {
					msg := tgbotapi.NewMessage(chatID, "Пользователи успешно удалены.")
					bot.Send(msg)
				}
				t.messageState[chatID] = ""
				continue
			}
		}

		// Проверка состояния логина
		if t.loginState[chatID] {
			if text == *secretWord {
				user := models.User{
					TelegramID: chatID,
					Username:   update.Message.Chat.UserName,
					FirstName:  update.Message.Chat.FirstName,
					LastName:   update.Message.Chat.LastName,
					Role:       "user",
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}

				t.adminMessageData[chatID] = &AdminMessageState{
					User: user,
				}

				t.loginState[chatID] = false
				t.loginAttempts[chatID] = 0
				t.messageState[chatID] = waitingBirthdateState

				msg := tgbotapi.NewMessage(chatID, "Введите вашу дату рождения в формате ДД.ММ.ГГГГ:")
				bot.Send(msg)
			} else {
				t.loginAttempts[chatID]++
				if t.loginAttempts[chatID] >= 3 {
					msg := tgbotapi.NewMessage(chatID, "Вы исчерпали количество попыток ввода секретного слова и заблокированы.")
					bot.Send(msg)
					t.loginState[chatID] = false
					t.loginAttempts[chatID] = 0

					// Обновляем состояние пользователя в базе данных
					err := t.userService.DeleteUsersByUsernames([]string{update.Message.Chat.UserName})
					if err != nil {
						log.Println("Error blocking user:", err)
					}
				} else {
					msg := tgbotapi.NewMessage(chatID, "Неправильное секретное слово, попробуйте снова.")
					bot.Send(msg)
				}
			}
			continue
		}

		switch text {

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
				t.messageState[chatID] = "waiting_message"
			} else {
				msg := tgbotapi.NewMessage(chatID, "У вас нет прав для использования этой команды.")
				bot.Send(msg)
			}

		case "/delete":
			user, err := t.userService.GetUser(models.User{TelegramID: chatID})
			if err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
				bot.Send(msg)
				continue
			}

			if user.Role == "admin" {
				msg := tgbotapi.NewMessage(chatID, "Введите логины пользователей через запятую, которых хотите заблокировать:")
				bot.Send(msg)
				t.messageState[chatID] = "waiting_delete_users"
			} else {
				msg := tgbotapi.NewMessage(chatID, "У вас нет прав для использования этой команды.")
				bot.Send(msg)
			}

		case "/list":

			user, err := t.userService.GetUser(models.User{TelegramID: chatID})
			if err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
				bot.Send(msg)
				continue
			}

			if user.Role == "admin" {
				users, err := t.userService.GetAllUsers()
				if err != nil {
					log.Println(err)
					msg := tgbotapi.NewMessage(chatID, "Ошибка при обновлении списка пользователей.")
					bot.Send(msg)
					continue
				}
				if len(users) == 0 {
					msg := tgbotapi.NewMessage(chatID, "Нет зарегистрированных пользователей.")
					t.Bot.Send(msg)
				}

				var userList string
				i := 1
				for _, user := range users {
					userList += fmt.Sprintf("%v. @%s\n", i, user.Username)
					i++
				}

				msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Список зарегистрированных пользователей:\n\n%s", userList))
				t.Bot.Send(msg)
			} else {
				msg := tgbotapi.NewMessage(chatID, "У вас нет прав для использования этой команды.")
				bot.Send(msg)
			}

		default:
			if update.Message.Text == "/start" || update.Message.From.UserName == "/start" {
				user := models.User{
					TelegramID: chatID,
				}

				user.Username = update.Message.Chat.UserName
				msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Привет, %s! Это простой телеграм бот для поздравляшек "+
					"своих близких коллег. Тут есть пару команд, чтобы ты мог начать получать сообщения! Если что-то будет "+
					"не так, то ты всегда можешь написать своему администратору для устранения проблем", user.Username))
				msg.ParseMode = "Markdown"
				bot.Send(msg)
				continue
			}
			msg := tgbotapi.NewMessage(chatID, "К сожалению, я вас не понял.")
			msg.ParseMode = "Markdown"
			bot.Send(msg)
		}
	}
	return bot
}

// Метод для отправки сообщений всем пользователям, кроме указанных
func (t *Telegram) sendMessageToUsers(adminID int64) {
	data := t.adminMessageData[adminID]
	if data == nil {
		msg := tgbotapi.NewMessage(adminID, "Ошибка при отправке сообщения.")
		t.Bot.Send(msg)
		return
	}

	users, err := t.userService.GetAllUsers()
	if err != nil {
		log.Println(err)
		msg := tgbotapi.NewMessage(adminID, "Ошибка при получении списка пользователей.")
		t.Bot.Send(msg)
		return
	}

	ignoredUsernames := map[string]struct{}{}
	for _, username := range data.IgnoredList {
		ignoredUsernames[username] = struct{}{}
	}

	for _, user := range users {
		if _, ignored := ignoredUsernames[user.Username]; !ignored {
			log.Printf("Sending message to user: %s", user.Username)
			msg := tgbotapi.NewMessage(user.TelegramID, data.Message)
			t.Bot.Send(msg)
		} else {
			log.Printf("Ignoring user: %s", user.Username)
		}
	}

	msg := tgbotapi.NewMessage(adminID, "Сообщение отправлено всем пользователям.")
	t.Bot.Send(msg)
	delete(t.adminMessageData, adminID)
}

func (t *Telegram) createUserSelectionKeyboard(users []models.User, addSendButton bool) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, user := range users {
		if !user.Blocked {
			button := tgbotapi.NewInlineKeyboardButtonData(user.Username, user.Username)
			row := tgbotapi.NewInlineKeyboardRow(button)
			rows = append(rows, row)
		}
	}

	if addSendButton {
		sendButton := tgbotapi.NewInlineKeyboardButtonData("Отправить", "send_message")
		sendRow := tgbotapi.NewInlineKeyboardRow(sendButton)
		rows = append(rows, sendRow)
	}

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (t *Telegram) NotifyUpcomingBirthdays() {
	usersWithBirthdayIn3Days, err := t.userService.GetUsersWithBirthdayInDays()
	if err != nil {
		log.Println("Error getting users with birthday in 3 days:", err)
		return
	}

	if len(usersWithBirthdayIn3Days) == 0 {
		return
	}

	admins, err := t.userService.GetAllAdmins()
	if err != nil {
		log.Println("Error getting all admins:", err)
		return
	}

	for _, birthdayUser := range usersWithBirthdayIn3Days {
		for _, admin := range admins {
			message := fmt.Sprintf("У нашего коллеги %s скоро день рождения! Не забудьте его поздравить!", birthdayUser.Username)
			msg := tgbotapi.NewMessage(admin.TelegramID, message)
			//fmt.Printf("Notifying admin %s about upcoming birthday of %s", admin.Username, birthdayUser.Username)
			t.Bot.Send(msg)
		}
	}
}
