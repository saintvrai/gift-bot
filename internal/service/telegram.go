package service

import (
	"database/sql"
	"fmt"
	"gift-bot/pkg/config"
	"gift-bot/pkg/models"
	"math/rand"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	log "github.com/sirupsen/logrus"
)

type Telegram struct {
	Bot              *tgbotapi.BotAPI
	userService      UserService
	loginAttempts    map[int64]int
	loginState       map[int64]bool
	blockedUsers     map[int64]time.Time
	messageState     map[int64]string             // Состояние: "waiting_message" или "waiting_ignored_users"
	adminMessageData map[int64]*AdminMessageState // Состояние сообщения администратора
	rateLimit        map[int64]*rateState
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
		rateLimit:        make(map[int64]*rateState),
	}
}

type AdminMessageState struct {
	Message     string
	IgnoredList []string
	User        models.User
	CurrentPage int
}

type rateState struct {
	windowStart time.Time
	count       int
	warned      bool
}

var (
	secretWord = &config.GlobalСonfig.Telegram.Secret
)

// Добавьте новое состояние для ожидания даты рождения
const waitingBirthdateState = "waiting_birthdate"
const waitingBlockUsersState = "waiting_block_users_select"
const waitingUnblockUsersState = "waiting_unblock_users_select"

func (t *Telegram) Start() *tgbotapi.BotAPI {
	bot := t.Bot

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil && update.CallbackQuery == nil {
			//log.Println("1", update.Message.From.UserName, update.Message.Chat.ID)
			continue
		}

		chatID, text := t.extractChatAndText(update)

		allow, warn := t.allowRequest(chatID)
		if !allow {
			if warn {
				msg := tgbotapi.NewMessage(chatID, "Слишком много запросов. Попробуйте позже.")
				bot.Send(msg)
			}
			continue
		}

		if t.handleStartCommand(bot, chatID, text) {
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
		if t.handleAdminMessageState(update, bot, chatID, text) {
			continue
		}

		// Проверка состояния логина
		if t.handleLoginState(update, bot, chatID, text) {
			continue
		}

		t.handleCommand(update, bot, chatID, text)
	}
	return bot
}

func (t *Telegram) allowRequest(chatID int64) (bool, bool) {
	const limit = 10
	window := time.Minute

	now := time.Now()
	state, ok := t.rateLimit[chatID]
	if !ok {
		t.rateLimit[chatID] = &rateState{windowStart: now, count: 1}
		return true, false
	}

	if now.Sub(state.windowStart) >= window {
		state.windowStart = now
		state.count = 1
		state.warned = false
		return true, false
	}

	if state.count >= limit {
		if state.warned {
			return false, false
		}
		state.warned = true
		return false, true
	}

	state.count++
	return true, false
}

func (t *Telegram) extractChatAndText(update tgbotapi.Update) (int64, string) {
	var chatID int64
	var text string

	if update.Message != nil {
		chatID = update.Message.Chat.ID
		text = update.Message.Text
		log.Println("2", chatID, update.Message.Text, update.Message.From.UserName, update.Message.From.FirstName)
	} else if update.CallbackQuery != nil {
		chatID = update.CallbackQuery.Message.Chat.ID
		text = update.CallbackQuery.Data
		//log.Println("3", update.Message.From.UserName, update.Message.Chat.ID)
	}

	return chatID, text
}

func (t *Telegram) handleStartCommand(bot *tgbotapi.BotAPI, chatID int64, text string) bool {
	if text != "/start" {
		return false
	}

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Привет! Это простой телеграм бот для поздравляшек "+
		"своих близких коллег. Тут есть пару команд, чтобы ты мог начать получать сообщения! Если что-то будет "+
		"не так, то ты всегда можешь написать своему администратору для устранения проблем.\n\n"+
		"Для начала введите команду /login"))
	msg.ParseMode = "Markdown"
	send, err := bot.Send(msg)
	if err != nil {
		log.Println("Error with start", send, err)
	}
	return true
}

func (t *Telegram) handleAdminMessageState(update tgbotapi.Update, bot *tgbotapi.BotAPI, chatID int64, text string) bool {
	if _, exists := t.messageState[chatID]; !exists {
		return false
	}

	if text == "отмена" {
		delete(t.adminMessageData, chatID)
		t.messageState[chatID] = ""
		msg := tgbotapi.NewMessage(chatID, "Действие отменено. Введите /message для отправки нового сообщения.")
		bot.Send(msg)
		return true
	}

	switch t.messageState[chatID] {
	case "waiting_message":
		log.Printf("Received message from admin: %s", text)
		t.adminMessageData[chatID] = &AdminMessageState{
			Message:     text,
			CurrentPage: 0,
		}
		t.messageState[chatID] = "waiting_ignored_users"

		users, err := t.userService.GetAllUsers()
		if err != nil {
			log.Println(err)
			msg := tgbotapi.NewMessage(chatID, "Ошибка при получении списка пользователей.")
			bot.Send(msg)
			return true
		}

		keyboard := t.createUserSelectionKeyboard(users, t.adminMessageData[chatID], true, "Отправить", "send_message", true)
		msg := tgbotapi.NewMessage(chatID, "Выберите пользователей, которым не нужно отправлять сообщение. Для отмены нажмите 'Отменить'.")
		msg.ReplyMarkup = keyboard
		bot.Send(msg)
		return true

	case "waiting_ignored_users":
		log.Printf("Received ignored users from admin: %s", text)
		if text == "отмена" {
			delete(t.adminMessageData, chatID)
			t.messageState[chatID] = ""
			msg := tgbotapi.NewMessage(chatID, "Действие отменено. Введите /message для отправки нового сообщения.")
			bot.Send(msg)
			return true
		}

		// Если это коллбэк от кнопки, добавляем пользователя в список игнорируемых
		if update.CallbackQuery != nil {
			if text == "send_message" {
				t.clearInlineKeyboard(bot, update)
				t.messageState[chatID] = ""
				t.sendMessageToUsers(chatID)
				return true
			}
			if text == "cancel_action" {
				t.clearInlineKeyboard(bot, update)
				delete(t.adminMessageData, chatID)
				t.messageState[chatID] = ""
				msg := tgbotapi.NewMessage(chatID, "Действие отменено.")
				bot.Send(msg)
				return true
			}
			if text == "noop" {
				return true
			}
			if text == "page:next" || text == "page:prev" {
				data := t.adminMessageData[chatID]
				if data == nil {
					return true
				}
				if text == "page:next" {
					data.CurrentPage++
				} else {
					data.CurrentPage--
				}

				users, err := t.userService.GetAllUsers()
				if err != nil {
					log.Println(err)
					msg := tgbotapi.NewMessage(chatID, "Ошибка при обновлении списка пользователей.")
					bot.Send(msg)
					return true
				}

				keyboard := t.createUserSelectionKeyboard(users, data, true, "Отправить", "send_message", true)
				editMsg := tgbotapi.NewEditMessageReplyMarkup(chatID, update.CallbackQuery.Message.MessageID, keyboard)
				bot.Send(editMsg)
				return true
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
				return true
			}

			keyboard := t.createUserSelectionKeyboard(users, data, true, "Отправить", "send_message", true)
			editMsg := tgbotapi.NewEditMessageReplyMarkup(chatID, update.CallbackQuery.Message.MessageID, keyboard)
			bot.Send(editMsg)
			return true
		}

		// Если администратор завершил выбор
		if text == "нет" {
			t.messageState[chatID] = ""
			t.sendMessageToUsers(chatID)
			return true
		}

	case "waiting_promote_admin":
		if text == "отмена" {
			t.messageState[chatID] = ""
			delete(t.adminMessageData, chatID)
			msg := tgbotapi.NewMessage(chatID, "Действие отменено.")
			bot.Send(msg)
			return true
		}

		if update.CallbackQuery != nil {
			if text == "noop" || text == "page:next" || text == "page:prev" {
				return t.handleAdminSelectionPaging(update, bot, chatID, "waiting_promote_admin")
			}
			if text == "cancel_action" {
				t.clearInlineKeyboard(bot, update)
				t.messageState[chatID] = ""
				delete(t.adminMessageData, chatID)
				msg := tgbotapi.NewMessage(chatID, "Действие отменено.")
				bot.Send(msg)
				return true
			}

			users, err := t.userService.GetAllUsers()
			if err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при получении списка пользователей.")
				bot.Send(msg)
				return true
			}

			var target *models.User
			for i := range users {
				if users[i].Username == text {
					target = &users[i]
					break
				}
			}

			if target == nil {
				msg := tgbotapi.NewMessage(chatID, "Пользователь не найден.")
				bot.Send(msg)
				return true
			}

			if target.Role == "admin" {
				msg := tgbotapi.NewMessage(chatID, "Пользователь уже админ.")
				bot.Send(msg)
				return true
			}

			target.Role = "admin"
			if err := t.userService.UpdateUser(*target); err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при назначении администратора.")
				bot.Send(msg)
				return true
			}

			t.clearInlineKeyboard(bot, update)

			t.messageState[chatID] = ""
			delete(t.adminMessageData, chatID)
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Пользователь @%s назначен администратором.", target.Username))
			bot.Send(msg)
			return true
		}

	case "waiting_demote_admin":
		if text == "отмена" {
			t.messageState[chatID] = ""
			delete(t.adminMessageData, chatID)
			msg := tgbotapi.NewMessage(chatID, "Действие отменено.")
			bot.Send(msg)
			return true
		}

		if update.CallbackQuery != nil {
			if text == "noop" || text == "page:next" || text == "page:prev" {
				return t.handleAdminSelectionPaging(update, bot, chatID, "waiting_demote_admin")
			}
			if text == "cancel_action" {
				t.clearInlineKeyboard(bot, update)
				t.messageState[chatID] = ""
				delete(t.adminMessageData, chatID)
				msg := tgbotapi.NewMessage(chatID, "Действие отменено.")
				bot.Send(msg)
				return true
			}

			users, err := t.userService.GetAllUsers()
			if err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при получении списка пользователей.")
				bot.Send(msg)
				return true
			}

			var target *models.User
			for i := range users {
				if users[i].Username == text && users[i].Role == "admin" {
					target = &users[i]
					break
				}
			}

			if target == nil {
				msg := tgbotapi.NewMessage(chatID, "Администратор не найден.")
				bot.Send(msg)
				return true
			}

			target.Role = "user"
			if err := t.userService.UpdateUser(*target); err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при снятии прав администратора.")
				bot.Send(msg)
				return true
			}

			t.clearInlineKeyboard(bot, update)

			t.messageState[chatID] = ""
			delete(t.adminMessageData, chatID)
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Пользователь @%s больше не администратор.", target.Username))
			bot.Send(msg)
			return true
		}

	case waitingBirthdateState:
		log.Printf("Received birthdate from user: %s", text)
		birthdate, err := time.Parse("02.01.2006", text)
		if err != nil {
			msg := tgbotapi.NewMessage(chatID, "Неверный формат даты. Пожалуйста, введите дату в формате ДД.ММ.ГГГГ:")
			bot.Send(msg)
			return true
		}

		user := t.adminMessageData[chatID].User
		user.Birthdate = birthdate
		err = t.userService.CreateUser(user)
		if err != nil {
			msg := tgbotapi.NewMessage(chatID, "Ошибка при создании пользователя.")
			bot.Send(msg)
			return true
		}

		delete(t.adminMessageData, chatID)
		t.messageState[chatID] = ""

		msg := tgbotapi.NewMessage(chatID, "Вы успешно зарегистрировались.")
		bot.Send(msg)
		// Отправляем админам уведомление о регистрации в боте пользователя

		admins, err := t.userService.GetAllAdmins()
		if err != nil {
			log.Println("Error getting all admins:", err)
			return true
		}

		for _, admin := range admins {
			message := fmt.Sprintf("Пользователь @%s зарегистрировался в боте", user.Username)
			msg := tgbotapi.NewMessage(admin.TelegramID, message)
			//fmt.Printf("Notifying admin %s about upcoming birthday of %s", admin.Username, birthdayUser.Username)
			t.Bot.Send(msg)
		}

		return true
	case waitingBlockUsersState:
		if text == "отмена" {
			t.messageState[chatID] = ""
			delete(t.adminMessageData, chatID)
			msg := tgbotapi.NewMessage(chatID, "Действие отменено.")
			bot.Send(msg)
			return true
		}

		if update.CallbackQuery != nil {
			if text == "noop" || text == "page:next" || text == "page:prev" {
				return t.handleAdminSelectionPaging(update, bot, chatID, waitingBlockUsersState)
			}
			if text == "cancel_action" {
				t.clearInlineKeyboard(bot, update)
				t.messageState[chatID] = ""
				delete(t.adminMessageData, chatID)
				msg := tgbotapi.NewMessage(chatID, "Действие отменено.")
				bot.Send(msg)
				return true
			}
			if text == "block_users" {
				data := t.adminMessageData[chatID]
				if data == nil || len(data.IgnoredList) == 0 {
					msg := tgbotapi.NewMessage(chatID, "Не выбраны пользователи для блокировки.")
					bot.Send(msg)
					return true
				}

				users, err := t.userService.GetAllUsers()
				if err != nil {
					log.Println(err)
					msg := tgbotapi.NewMessage(chatID, "Ошибка при получении списка пользователей.")
					bot.Send(msg)
					return true
				}

				byUsername := make(map[string]models.User, len(users))
				for _, u := range users {
					byUsername[u.Username] = u
				}

				err = t.userService.DeleteUsersByUsernames(data.IgnoredList)
				if err != nil {
					log.Println(err)
					msg := tgbotapi.NewMessage(chatID, "Ошибка при блокировке пользователей.")
					bot.Send(msg)
					return true
				}

				var blockedList []string
				for _, username := range data.IgnoredList {
					if u, ok := byUsername[username]; ok {
						blockedList = append(blockedList, formatUserButtonText(u))
					} else {
						blockedList = append(blockedList, "@"+strings.TrimSpace(username))
					}
				}

				t.clearInlineKeyboard(bot, update)
				t.messageState[chatID] = ""
				delete(t.adminMessageData, chatID)
				msgText := "Пользователи успешно заблокированы:\n" + strings.Join(blockedList, "\n")
				msg := tgbotapi.NewMessage(chatID, msgText)
				bot.Send(msg)
				return true
			}

			username := update.CallbackQuery.Data
			data := t.adminMessageData[chatID]
			if data == nil {
				return true
			}
			data.IgnoredList = append(data.IgnoredList, username)

			users, err := t.userService.GetAllUsers()
			if err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при обновлении списка пользователей.")
				bot.Send(msg)
				return true
			}

			keyboard := t.createUserSelectionKeyboard(users, data, true, "Заблокировать", "block_users", true)
			editMsg := tgbotapi.NewEditMessageReplyMarkup(chatID, update.CallbackQuery.Message.MessageID, keyboard)
			bot.Send(editMsg)
			return true
		}

	case waitingUnblockUsersState:
		if text == "отмена" {
			t.messageState[chatID] = ""
			delete(t.adminMessageData, chatID)
			msg := tgbotapi.NewMessage(chatID, "Действие отменено.")
			bot.Send(msg)
			return true
		}

		if update.CallbackQuery != nil {
			if text == "noop" || text == "page:next" || text == "page:prev" {
				return t.handleAdminSelectionPaging(update, bot, chatID, waitingUnblockUsersState)
			}
			if text == "cancel_action" {
				t.clearInlineKeyboard(bot, update)
				t.messageState[chatID] = ""
				delete(t.adminMessageData, chatID)
				msg := tgbotapi.NewMessage(chatID, "Действие отменено.")
				bot.Send(msg)
				return true
			}
			if text == "unblock_users" {
				data := t.adminMessageData[chatID]
				if data == nil || len(data.IgnoredList) == 0 {
					msg := tgbotapi.NewMessage(chatID, "Не выбраны пользователи для разблокировки.")
					bot.Send(msg)
					return true
				}

				users, err := t.userService.GetBlockedUsers()
				if err != nil {
					log.Println(err)
					msg := tgbotapi.NewMessage(chatID, "Ошибка при получении списка пользователей.")
					bot.Send(msg)
					return true
				}

				byUsername := make(map[string]models.User, len(users))
				for _, u := range users {
					byUsername[u.Username] = u
				}

				err = t.userService.UnblockUsersByUsernames(data.IgnoredList)
				if err != nil {
					log.Println(err)
					msg := tgbotapi.NewMessage(chatID, "Ошибка при разблокировке пользователей.")
					bot.Send(msg)
					return true
				}

				var unblockedList []string
				for _, username := range data.IgnoredList {
					if u, ok := byUsername[username]; ok {
						unblockedList = append(unblockedList, formatUserButtonText(u))
					} else {
						unblockedList = append(unblockedList, "@"+strings.TrimSpace(username))
					}
				}

				t.clearInlineKeyboard(bot, update)
				t.messageState[chatID] = ""
				delete(t.adminMessageData, chatID)
				msgText := "Пользователи успешно разблокированы:\n" + strings.Join(unblockedList, "\n")
				msg := tgbotapi.NewMessage(chatID, msgText)
				bot.Send(msg)
				return true
			}

			username := update.CallbackQuery.Data
			data := t.adminMessageData[chatID]
			if data == nil {
				return true
			}
			data.IgnoredList = append(data.IgnoredList, username)

			users, err := t.userService.GetBlockedUsers()
			if err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при обновлении списка пользователей.")
				bot.Send(msg)
				return true
			}

			keyboard := t.createUserSelectionKeyboard(users, data, true, "Разблокировать", "unblock_users", true)
			editMsg := tgbotapi.NewEditMessageReplyMarkup(chatID, update.CallbackQuery.Message.MessageID, keyboard)
			bot.Send(editMsg)
			return true
		}
	}

	return false
}

func (t *Telegram) handleAdminSelectionPaging(update tgbotapi.Update, bot *tgbotapi.BotAPI, chatID int64, state string) bool {
	if update.CallbackQuery == nil {
		return true
	}

	text := update.CallbackQuery.Data
	if text == "noop" {
		return true
	}

	data := t.adminMessageData[chatID]
	if data == nil {
		return true
	}

	if text == "page:next" {
		data.CurrentPage++
	} else if text == "page:prev" {
		data.CurrentPage--
	}

	var users []models.User
	var err error
	if state == waitingUnblockUsersState {
		users, err = t.userService.GetBlockedUsers()
	} else {
		users, err = t.userService.GetAllUsers()
	}
	if err != nil {
		log.Println(err)
		msg := tgbotapi.NewMessage(chatID, "Ошибка при обновлении списка пользователей.")
		bot.Send(msg)
		return true
	}

	var filtered []models.User
	switch state {
	case "waiting_promote_admin":
		for _, user := range users {
			if user.Role != "admin" {
				filtered = append(filtered, user)
			}
		}
	case "waiting_demote_admin":
		for _, user := range users {
			if user.Role == "admin" {
				filtered = append(filtered, user)
			}
		}
	case waitingBlockUsersState:
		filtered = users
	case waitingUnblockUsersState:
		filtered = users
	default:
		filtered = users
	}

	actionLabel, actionCallback := "", ""
	if state == waitingBlockUsersState {
		actionLabel = "Заблокировать"
		actionCallback = "block_users"
	} else if state == waitingUnblockUsersState {
		actionLabel = "Разблокировать"
		actionCallback = "unblock_users"
	}

	keyboard := t.createUserSelectionKeyboard(filtered, data, actionLabel != "", actionLabel, actionCallback, true)
	editMsg := tgbotapi.NewEditMessageReplyMarkup(chatID, update.CallbackQuery.Message.MessageID, keyboard)
	bot.Send(editMsg)
	return true
}

func (t *Telegram) handleLoginState(update tgbotapi.Update, bot *tgbotapi.BotAPI, chatID int64, text string) bool {
	if !t.loginState[chatID] {
		return false
	}

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

	return true
}

func (t *Telegram) clearInlineKeyboard(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.Message == nil {
		return
	}

	editMsg := tgbotapi.NewEditMessageReplyMarkup(
		update.CallbackQuery.Message.Chat.ID,
		update.CallbackQuery.Message.MessageID,
		tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
	)
	if _, err := bot.Send(editMsg); err != nil {
		log.Printf("Error clearing inline keyboard: %v", err)
	}
}

func (t *Telegram) startBlockUsersFlow(chatID int64, bot *tgbotapi.BotAPI) {
	users, err := t.userService.GetAllUsers()
	if err != nil {
		log.Println(err)
		msg := tgbotapi.NewMessage(chatID, "Ошибка при получении списка пользователей.")
		bot.Send(msg)
		return
	}

	if len(users) == 0 {
		msg := tgbotapi.NewMessage(chatID, "Нет пользователей для блокировки.")
		bot.Send(msg)
		return
	}

	t.adminMessageData[chatID] = &AdminMessageState{CurrentPage: 0, IgnoredList: []string{}}
	t.messageState[chatID] = waitingBlockUsersState

	keyboard := t.createUserSelectionKeyboard(users, t.adminMessageData[chatID], true, "Заблокировать", "block_users", true)
	msg := tgbotapi.NewMessage(chatID, "Выберите пользователей для блокировки. Для отмены нажмите 'Отменить'.")
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func (t *Telegram) startUnblockUsersFlow(chatID int64, bot *tgbotapi.BotAPI) {
	users, err := t.userService.GetBlockedUsers()
	if err != nil {
		log.Println(err)
		msg := tgbotapi.NewMessage(chatID, "Ошибка при получении списка пользователей.")
		bot.Send(msg)
		return
	}

	if len(users) == 0 {
		msg := tgbotapi.NewMessage(chatID, "Нет заблокированных пользователей.")
		bot.Send(msg)
		return
	}

	t.adminMessageData[chatID] = &AdminMessageState{CurrentPage: 0, IgnoredList: []string{}}
	t.messageState[chatID] = waitingUnblockUsersState

	keyboard := t.createUserSelectionKeyboard(users, t.adminMessageData[chatID], true, "Разблокировать", "unblock_users", true)
	msg := tgbotapi.NewMessage(chatID, "Выберите пользователей для разблокировки. Для отмены нажмите 'Отменить'.")
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func (t *Telegram) handleCommand(update tgbotapi.Update, bot *tgbotapi.BotAPI, chatID int64, text string) {
	if update.Message == nil {
		return
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
			return
		}

		if existingUser.TelegramID != 0 {
			msg := tgbotapi.NewMessage(chatID, "Вы уже зарегистрированы в боте.")
			bot.Send(msg)
			return
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
			return
		}

		if user.Role == "admin" {
			msg := tgbotapi.NewMessage(chatID, "Введите сообщение, которое хотите отправить всем пользователям:")
			bot.Send(msg)
			t.messageState[chatID] = "waiting_message"
		} else {
			msg := tgbotapi.NewMessage(chatID, "У вас нет прав для использования этой команды.")
			bot.Send(msg)
		}

	case "/block":
		user, err := t.userService.GetUser(models.User{TelegramID: chatID})
		if err != nil {
			log.Println(err)
			msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
			bot.Send(msg)
			return
		}

		if user.Role == "admin" {
			t.startBlockUsersFlow(chatID, bot)
		} else {
			msg := tgbotapi.NewMessage(chatID, "У вас нет прав для использования этой команды.")
			bot.Send(msg)
		}

	case "/unblock":
		user, err := t.userService.GetUser(models.User{TelegramID: chatID})
		if err != nil {
			log.Println(err)
			msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
			bot.Send(msg)
			return
		}

		if user.Role == "admin" {
			t.startUnblockUsersFlow(chatID, bot)
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
			return
		}

		if user.Role == "admin" {
			users, err := t.userService.GetAllUsers()
			if err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при обновлении списка пользователей.")
				bot.Send(msg)
				return
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

	case "/help":
		helpText := "Доступные команды:\n" +
			"/start — приветствие\n" +
			"/help — список команд\n" +
			"/chat — показать ID чата\n" +
			"/login — регистрация в боте\n\n" +
			"Команды только для админов:\n" +
			"/message — рассылка сообщения пользователям\n" +
			"/block — заблокировать пользователей\n" +
			"/unblock — разблокировать пользователей\n" +
			"/list — список зарегистрированных пользователей\n" +
			"/admin_add — назначить администратора\n" +
			"/admin_remove — снять права администратора"
		msg := tgbotapi.NewMessage(chatID, helpText)
		bot.Send(msg)

	case "/admin_add":
		user, err := t.userService.GetUser(models.User{TelegramID: chatID})
		if err != nil {
			log.Println(err)
			msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
			bot.Send(msg)
			return
		}

		if user.Role != "admin" {
			msg := tgbotapi.NewMessage(chatID, "У вас нет прав для использования этой команды.")
			bot.Send(msg)
			return
		}

		users, err := t.userService.GetAllUsers()
		if err != nil {
			log.Println(err)
			msg := tgbotapi.NewMessage(chatID, "Ошибка при получении списка пользователей.")
			bot.Send(msg)
			return
		}

		var candidates []models.User
		for _, u := range users {
			if u.Role != "admin" {
				candidates = append(candidates, u)
			}
		}

		if len(candidates) == 0 {
			msg := tgbotapi.NewMessage(chatID, "Нет пользователей для назначения администратора.")
			bot.Send(msg)
			return
		}

		t.adminMessageData[chatID] = &AdminMessageState{CurrentPage: 0}
		t.messageState[chatID] = "waiting_promote_admin"

		keyboard := t.createUserSelectionKeyboard(candidates, t.adminMessageData[chatID], false, "", "", true)
		msg := tgbotapi.NewMessage(chatID, "Выберите пользователя, которого нужно назначить администратором. Для отмены нажмите 'Отменить'.")
		msg.ReplyMarkup = keyboard
		bot.Send(msg)

	case "/admin_remove":
		user, err := t.userService.GetUser(models.User{TelegramID: chatID})
		if err != nil {
			log.Println(err)
			msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
			bot.Send(msg)
			return
		}

		if user.Role != "admin" {
			msg := tgbotapi.NewMessage(chatID, "У вас нет прав для использования этой команды.")
			bot.Send(msg)
			return
		}

		users, err := t.userService.GetAllUsers()
		if err != nil {
			log.Println(err)
			msg := tgbotapi.NewMessage(chatID, "Ошибка при получении списка пользователей.")
			bot.Send(msg)
			return
		}

		var admins []models.User
		for _, u := range users {
			if u.Role == "admin" {
				admins = append(admins, u)
			}
		}

		if len(admins) == 0 {
			msg := tgbotapi.NewMessage(chatID, "Нет администраторов для снятия прав.")
			bot.Send(msg)
			return
		}

		t.adminMessageData[chatID] = &AdminMessageState{CurrentPage: 0}
		t.messageState[chatID] = "waiting_demote_admin"

		keyboard := t.createUserSelectionKeyboard(admins, t.adminMessageData[chatID], false, "", "", true)
		msg := tgbotapi.NewMessage(chatID, "Выберите администратора, которому нужно снять права. Для отмены нажмите 'Отменить'.")
		msg.ReplyMarkup = keyboard
		bot.Send(msg)

	default:
		if update.Message.Text == "/start" || update.Message.From.UserName == "/start" {
			user := models.User{
				TelegramID: chatID,
			}

			user.Username = update.Message.Chat.UserName
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Привет, %s! Это простой телеграм бот для поздравляшек "+
				"своих близких коллег. Тут есть пару команд, чтобы ты мог начать получать сообщения! Если что-то будет "+
				"не так, то ты всегда можешь написать своему администратору для устранения проблем.\n\n"+
				"Для начала введите команду /login", user.Username))
			msg.ParseMode = "Markdown"
			bot.Send(msg)
			return
		}
		msg := tgbotapi.NewMessage(chatID, "К сожалению, я вас не понял.")
		msg.ParseMode = "Markdown"
		bot.Send(msg)
	}
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

func (t *Telegram) createUserSelectionKeyboard(users []models.User, data *AdminMessageState, addActionButton bool, actionLabel string, actionCallback string, addCancelButton bool) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	filteredUsers := filterUsersByIgnored(users, data)
	const pageSize = 10
	totalPages := (len(filteredUsers) + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}
	if data != nil {
		if data.CurrentPage < 0 {
			data.CurrentPage = 0
		}
		if data.CurrentPage >= totalPages {
			data.CurrentPage = totalPages - 1
		}
	}

	currentPage := 0
	if data != nil {
		currentPage = data.CurrentPage
	}

	start := currentPage * pageSize
	end := start + pageSize
	if start > len(filteredUsers) {
		start = len(filteredUsers)
	}
	if end > len(filteredUsers) {
		end = len(filteredUsers)
	}

	for _, user := range filteredUsers[start:end] {
		buttonText := formatUserButtonText(user)
		button := tgbotapi.NewInlineKeyboardButtonData(buttonText, user.Username)
		row := tgbotapi.NewInlineKeyboardRow(button)
		rows = append(rows, row)
	}

	if totalPages > 1 {
		var navRow []tgbotapi.InlineKeyboardButton
		if currentPage > 0 {
			navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("<<", "page:prev"))
		}
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("Стр. %d/%d", currentPage+1, totalPages), "noop"))
		if currentPage < totalPages-1 {
			navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData(">>", "page:next"))
		}
		rows = append(rows, navRow)
	}

	if addActionButton {
		buttonText := strings.TrimSpace(actionLabel)
		buttonData := strings.TrimSpace(actionCallback)
		if buttonText != "" && buttonData != "" {
			actionButton := tgbotapi.NewInlineKeyboardButtonData(buttonText, buttonData)
			actionRow := tgbotapi.NewInlineKeyboardRow(actionButton)
			rows = append(rows, actionRow)
		}
	}

	if addCancelButton {
		cancelButton := tgbotapi.NewInlineKeyboardButtonData("Отменить", "cancel_action")
		cancelRow := tgbotapi.NewInlineKeyboardRow(cancelButton)
		rows = append(rows, cancelRow)
	}

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func filterUsersByIgnored(users []models.User, data *AdminMessageState) []models.User {
	if data == nil || len(data.IgnoredList) == 0 {
		return users
	}

	ignored := make(map[string]struct{}, len(data.IgnoredList))
	for _, username := range data.IgnoredList {
		ignored[username] = struct{}{}
	}

	var filtered []models.User
	for _, user := range users {
		if _, skip := ignored[user.Username]; !skip {
			filtered = append(filtered, user)
		}
	}
	return filtered
}

func formatUserButtonText(user models.User) string {
	username := strings.TrimSpace(user.Username)
	firstName := strings.TrimSpace(user.FirstName)
	lastName := strings.TrimSpace(user.LastName)

	display := "@" + username
	name := strings.TrimSpace(strings.Join([]string{firstName, lastName}, " "))
	if name != "" {
		display += " — " + name
	}
	return display
}

func (t *Telegram) NotifyUpcomingBirthdays() {
	now := time.Now().In(time.Local)
	notifyDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

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
			sent, err := t.userService.HasBirthdayNotification(admin.TelegramID, birthdayUser.TelegramID, notifyDate)
			if err != nil {
				log.Println("Error checking birthday notification:", err)
				continue
			}
			if sent {
				continue
			}

			message := fmt.Sprintf("У нашего коллеги @%s скоро день рождения! Не забудьте его поздравить!", birthdayUser.Username)
			msg := tgbotapi.NewMessage(admin.TelegramID, message)
			//fmt.Printf("Notifying admin %s about upcoming birthday of %s", admin.Username, birthdayUser.Username)
			if _, err := t.Bot.Send(msg); err != nil {
				log.Printf("Error notifying admin %s about birthday of %s: %v", admin.Username, birthdayUser.Username, err)
				continue
			}

			if err := t.userService.SaveBirthdayNotification(admin.TelegramID, birthdayUser.TelegramID, notifyDate); err != nil {
				log.Println("Error saving birthday notification:", err)
			}
		}
	}
}

func (t *Telegram) SyncUserProfiles() {
	users, err := t.userService.GetAllUsers()
	if err != nil {
		log.Println("Error getting users for profile sync:", err)
		return
	}

	if len(users) == 0 {
		return
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	baseDelay := 250 * time.Millisecond
	jitter := 250 * time.Millisecond

	for _, user := range users {
		chat, err := t.Bot.GetChat(tgbotapi.ChatInfoConfig{
			ChatConfig: tgbotapi.ChatConfig{ChatID: user.TelegramID},
		})
		if err != nil {
			log.Printf("Error getting chat %d: %v", user.TelegramID, err)
			time.Sleep(baseDelay + time.Duration(rng.Intn(int(jitter.Milliseconds())))*time.Millisecond)
			continue
		}

		newUsername := chat.UserName
		newFirstName := chat.FirstName
		newLastName := chat.LastName

		if user.Username != newUsername || user.FirstName != newFirstName || user.LastName != newLastName {
			user.Username = newUsername
			user.FirstName = newFirstName
			user.LastName = newLastName

			if err := t.userService.UpdateUser(user); err != nil {
				log.Printf("Error updating user %d: %v", user.TelegramID, err)
			}
		}

		time.Sleep(baseDelay + time.Duration(rng.Intn(int(jitter.Milliseconds())))*time.Millisecond)
	}
}
