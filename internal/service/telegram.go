package service

import (
	"database/sql"
	"fmt"
	"gift-bot/pkg/config"
	"gift-bot/pkg/models"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	log "github.com/sirupsen/logrus"
	"strconv"
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
	wishlistState    map[int64]string             // Состояние: "waiting_addwish" или "waiting_removewish"
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
		wishlistState:    make(map[int64]string),
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
			continue
		}

		var chatID int64
		var text string

		if update.Message != nil {
			chatID = update.Message.Chat.ID
			text = update.Message.Text
			log.Println("Message from:", chatID, update.Message.Text, update.Message.From.UserName, update.Message.From.FirstName)
		} else if update.CallbackQuery != nil {
			chatID = update.CallbackQuery.Message.Chat.ID
			text = update.CallbackQuery.Data
			log.Println("Callback from:", chatID, update.CallbackQuery.Data)
			// Ответ на CallbackQuery, чтобы убрать "часики"
			callback := tgbotapi.NewCallback(update.CallbackQuery.ID, "")
			if _, err := bot.Request(callback); err != nil {
				log.Errorf("Failed to send callback response: %v", err)
			}
			// Обработка CallbackQuery отдельно
			t.handleCallbackQuery(update.CallbackQuery)
			continue
		}

		if text == "/start" {
			msg := tgbotapi.NewMessage(chatID, "Привет! Это простой телеграм бот для поздравлений своих близких коллег. Вот доступные команды:\n"+
				"/wishlist — управление вашими желаниями\n"+
				"/login — регистрация\n"+
				"/chat — получить ваш уникальный номер чата\n"+
				"/message — отправить сообщение (для админов)\n"+
				"/delete — заблокировать пользователей (для админов)\n"+
				"/list — список пользователей (для админов)\n"+
				"/viewwishlist — показать список желаний пользователя (для админов)")
			msg.ParseMode = "Markdown"
			send, err := bot.Send(msg)
			if err != nil {
				log.Println("Error with /start command:", send, err)
			}
			continue
		}

		// Проверяем, заблокирован ли пользователь
		existingUser, err := t.userService.GetUser(models.User{TelegramID: chatID})
		if err != nil && err != sql.ErrNoRows {
			log.Errorf("Error getting existing user: %v", err)
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

		// Обработка состояний wishlist
		if state, exists := t.wishlistState[chatID]; exists {
			switch state {
			case "waiting_addwish":
				t.processAddWish(chatID, text)
			case "waiting_removewish":
				t.processRemoveWish(chatID, text)
			}
			continue
		}

		// Обработка команд
		switch text {
		case "/wishlist":
			keyboard := t.createWishlistMenuKeyboard()
			msg := tgbotapi.NewMessage(chatID, "Выберите действие для управления вашим списком желаний:")
			msg.ReplyMarkup = keyboard
			bot.Send(msg)
			continue

		case "Просмотреть":
			t.handleViewWishlist(chatID)
			continue

		case "Добавить":
			t.handleAddWishlist(chatID)
			continue

		case "Удалить":
			t.handleRemoveWishlist(chatID)
			continue

		case "Назад":
			t.showMainMenu(chatID)
			continue

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

		case "/viewwishlist":
			user, err := t.userService.GetUser(models.User{TelegramID: chatID})
			if err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
				bot.Send(msg)
				continue
			}

			if user.Role != "admin" {
				msg := tgbotapi.NewMessage(chatID, "У вас нет прав для использования этой команды.")
				bot.Send(msg)
				continue
			}

			users, err := t.userService.GetAllUsers()
			if err != nil {
				log.Println(err)
				msg := tgbotapi.NewMessage(chatID, "Ошибка при получении списка пользователей.")
				bot.Send(msg)
				continue
			}

			if len(users) == 0 {
				msg := tgbotapi.NewMessage(chatID, "Нет зарегистрированных пользователей.")
				bot.Send(msg)
				continue
			}

			// Создаем inline-клавиатуру для выбора пользователя
			keyboard := t.createUserSelectionKeyboardForWishlist(users)
			msg := tgbotapi.NewMessage(chatID, "Выберите пользователя, wishlist которого хотите просмотреть:")
			msg.ReplyMarkup = keyboard
			bot.Send(msg)

		default:
			// Обработка неизвестных команд
			if update.Message != nil && (update.Message.Text == "/start" || update.Message.From.UserName == "/start") {
				user := models.User{
					TelegramID: chatID,
				}

				user.Username = update.Message.Chat.UserName
				msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Привет, %s! Это простой телеграм бот для поздравлений своих близких коллег. Вот доступные команды:\n"+
					"/wishlist — управление вашими желаниями\n"+
					"/login — регистрация\n"+
					"/chat — получить ваш уникальный номер чата\n"+
					"/message — отправить сообщение (для админов)\n"+
					"/delete — заблокировать пользователей (для админов)\n"+
					"/list — список пользователей (для админов)\n"+
					"/viewwishlist — показать список желаний пользователя (для админов)", user.Username))
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

// Метод для создания меню wishlist с использованием ReplyKeyboardMarkup
func (t *Telegram) createWishlistMenuKeyboard() tgbotapi.ReplyKeyboardMarkup {
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Просмотреть"),
			tgbotapi.NewKeyboardButton("Добавить"),
			tgbotapi.NewKeyboardButton("Удалить"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("Назад"),
		),
	)
	return keyboard
}

// Обработка просмотра wishlist
func (t *Telegram) handleViewWishlist(chatID int64) {
	user, err := t.userService.GetUser(models.User{TelegramID: chatID})
	if err != nil {
		log.Errorf("Error getting user: %v", err)
		msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
		t.Bot.Send(msg)
		return
	}

	if len(user.Wishlist) == 0 {
		msg := tgbotapi.NewMessage(chatID, "Ваш список желаний пуст.")
		t.Bot.Send(msg)
		return
	}

	var wishlist strings.Builder
	wishlist.WriteString("Ваш список желаний:\n")
	for i, wish := range user.Wishlist {
		wishlist.WriteString(fmt.Sprintf("%d. %s\n", i+1, wish))
	}

	msg := tgbotapi.NewMessage(chatID, wishlist.String())
	t.Bot.Send(msg)
}

// Обработка добавления wishlist
func (t *Telegram) handleAddWishlist(chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "Введите ваше желание:")
	t.Bot.Send(msg)
	t.wishlistState[chatID] = "waiting_addwish"
}

// Обработка удаления wishlist по номеру
func (t *Telegram) handleRemoveWishlist(chatID int64) {
	user, err := t.userService.GetUser(models.User{TelegramID: chatID})
	if err != nil {
		log.Errorf("Error getting user: %v", err)
		msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
		t.Bot.Send(msg)
		return
	}

	if len(user.Wishlist) == 0 {
		msg := tgbotapi.NewMessage(chatID, "Ваш список желаний пуст.")
		t.Bot.Send(msg)
		return
	}

	var wishlist strings.Builder
	wishlist.WriteString("Ваш список желаний:\n")
	for i, wish := range user.Wishlist {
		wishlist.WriteString(fmt.Sprintf("%d. %s\n", i+1, wish))
	}
	wishlist.WriteString("\nВведите номер желания, которое хотите удалить:")

	msg := tgbotapi.NewMessage(chatID, wishlist.String())
	t.Bot.Send(msg)
	t.wishlistState[chatID] = "waiting_removewish"
}

// Обработка перехода назад в главное меню
func (t *Telegram) showMainMenu(chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "Вы вернулись в главное меню. Вот доступные команды:\n"+
		"/wishlist — управление вашими желаниями\n"+
		"/login — регистрация\n"+
		"/chat — получить ваш уникальный номер чата\n"+
		"/message — отправить сообщение (для админов)\n"+
		"/delete — заблокировать пользователей (для админов)\n"+
		"/list — список пользователей (для админов)\n"+
		"/viewwishlist — показать список желаний пользователя (для админов)")
	// Удаляем клавиатуру и возвращаемся к основному меню
	msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
	t.Bot.Send(msg)
}

// Обработка добавления желания
func (t *Telegram) processAddWish(chatID int64, wish string) {
	wish = strings.TrimSpace(wish)
	if wish == "" {
		msg := tgbotapi.NewMessage(chatID, "Желание не может быть пустым. Попробуйте снова.")
		t.Bot.Send(msg)
		return
	}

	user, err := t.userService.GetUser(models.User{TelegramID: chatID})
	if err != nil {
		log.Errorf("Error getting user: %v", err)
		msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
		t.Bot.Send(msg)
		return
	}

	user.Wishlist = append(user.Wishlist, wish)
	err = t.userService.UpdateUser(user)
	if err != nil {
		log.Errorf("Error updating user wishlist: %v", err)
		msg := tgbotapi.NewMessage(chatID, "Ошибка при обновлении вашего списка желаний.")
		t.Bot.Send(msg)
		return
	}

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Желание \"%s\" добавлено в ваш список.", wish))
	t.Bot.Send(msg)
	delete(t.wishlistState, chatID)

	// Показать меню снова
	keyboard := t.createWishlistMenuKeyboard()
	msg = tgbotapi.NewMessage(chatID, "Выберите следующее действие:")
	msg.ReplyMarkup = keyboard
	t.Bot.Send(msg)
}

// Обработка удаления wishlist по номеру
func (t *Telegram) processRemoveWish(chatID int64, input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		msg := tgbotapi.NewMessage(chatID, "Номер не может быть пустым. Попробуйте снова.")
		t.Bot.Send(msg)
		return
	}

	// Обработка специального случая "Назад"
	if strings.ToLower(input) == "назад" {
		t.showMainMenu(chatID)
		delete(t.wishlistState, chatID)
		return
	}

	// Проверяем, является ли ввод числом
	wishNumber, err := strconv.Atoi(input)
	if err != nil || wishNumber < 1 {
		msg := tgbotapi.NewMessage(chatID, "Пожалуйста, введите корректный номер желания.")
		t.Bot.Send(msg)
		return
	}

	user, err := t.userService.GetUser(models.User{TelegramID: chatID})
	if err != nil {
		log.Errorf("Error getting user: %v", err)
		msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
		t.Bot.Send(msg)
		return
	}

	if wishNumber > len(user.Wishlist) {
		msg := tgbotapi.NewMessage(chatID, "Желания с таким номером не существует.")
		t.Bot.Send(msg)
		return
	}

	// Удаляем желание по номеру
	removedWish := user.Wishlist[wishNumber-1]
	user.Wishlist = append(user.Wishlist[:wishNumber-1], user.Wishlist[wishNumber:]...)
	err = t.userService.UpdateUser(user)
	if err != nil {
		log.Errorf("Error updating user wishlist: %v", err)
		msg := tgbotapi.NewMessage(chatID, "Ошибка при обновлении вашего списка желаний.")
		t.Bot.Send(msg)
		return
	}

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Желание \"%s\" удалено из вашего списка.", removedWish))
	t.Bot.Send(msg)
	delete(t.wishlistState, chatID)

	// Показать меню снова
	keyboard := t.createWishlistMenuKeyboard()
	msg = tgbotapi.NewMessage(chatID, "Выберите следующее действие:")
	msg.ReplyMarkup = keyboard
	t.Bot.Send(msg)
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
		// Получаем пользователя с полным списком желаний
		user, err := t.userService.GetUser(models.User{TelegramID: birthdayUser.TelegramID})
		if err != nil {
			log.Errorf("Error getting user details for TelegramID %d: %v", birthdayUser.TelegramID, err)
			continue
		}

		// Формируем сообщение о дне рождения
		baseMessage := fmt.Sprintf("🎉 У нашего коллеги @%s скоро день рождения! Не забудьте его поздравить!\n\n", user.Username)

		// Добавляем список желаний, если он существует
		if len(user.Wishlist) > 0 {
			var wishlistBuilder strings.Builder
			wishlistBuilder.WriteString("🎁 *Список желаний пользователя:*\n")
			for i, wish := range user.Wishlist {
				wishlistBuilder.WriteString(fmt.Sprintf("%d. %s\n", i+1, wish))
			}
			baseMessage += wishlistBuilder.String()
		} else {
			baseMessage += "📋 У пользователя нет списка желаний."
		}

		// Отправляем сообщение каждому администратору
		for _, admin := range admins {
			msg := tgbotapi.NewMessage(admin.TelegramID, baseMessage)
			msg.ParseMode = "Markdown"
			if _, err := t.Bot.Send(msg); err != nil {
				log.Errorf("Failed to send birthday notification to admin %d: %v", admin.TelegramID, err)
			}
		}
	}
}

// Обработка CallbackQuery для администратора
func (t *Telegram) handleCallbackQuery(callback *tgbotapi.CallbackQuery) {
	bot := t.Bot
	chatID := callback.Message.Chat.ID
	data := callback.Data

	// Проверяем префикс callback-запроса
	if strings.HasPrefix(data, "view_wishlist:") {
		// Обработка запроса на просмотр wishlist
		telegramIDStr := strings.TrimPrefix(data, "view_wishlist:")
		telegramID, err := strconv.ParseInt(telegramIDStr, 10, 64)
		if err != nil {
			log.Errorf("Invalid TelegramID in callback data: %v", err)
			msg := tgbotapi.NewMessage(chatID, "Произошла ошибка при обработке запроса.")
			bot.Send(msg)
			return
		}

		user, err := t.userService.GetUser(models.User{TelegramID: telegramID})
		if err != nil {
			log.Errorf("Error getting user with TelegramID %d: %v", telegramID, err)
			msg := tgbotapi.NewMessage(chatID, "Ошибка при получении данных пользователя.")
			bot.Send(msg)
			return
		}

		if len(user.Wishlist) == 0 {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Wishlist пользователя @%s пуст.", user.Username))
			bot.Send(msg)
			return
		}

		var wishlist strings.Builder
		wishlist.WriteString(fmt.Sprintf("Wishlist пользователя @%s:\n", user.Username))
		for i, wish := range user.Wishlist {
			wishlist.WriteString(fmt.Sprintf("%d. %s\n", i+1, wish))
		}

		msg := tgbotapi.NewMessage(chatID, wishlist.String())
		bot.Send(msg)
		return
	}

	// Проверяем состояние администратора
	state, exists := t.messageState[chatID]
	if !exists {
		// Не в состоянии обработки сообщений
		return
	}

	switch state {
	case "waiting_ignored_users":
		if data == "send_message" {
			t.messageState[chatID] = ""
			t.sendMessageToUsers(chatID)
			return
		}

		username := data
		dataState := t.adminMessageData[chatID]
		dataState.IgnoredList = append(dataState.IgnoredList, username)

		// Обновляем клавиатуру, чтобы удалить выбранного пользователя
		users, err := t.userService.GetAllUsers()
		if err != nil {
			log.Println(err)
			msg := tgbotapi.NewMessage(chatID, "Ошибка при обновлении списка пользователей.")
			bot.Send(msg)
			return
		}

		// Удаляем уже выбранных пользователей из клавиатуры
		var remainingUsers []models.User
		for _, user := range users {
			ignored := false
			for _, ignoredUser := range dataState.IgnoredList {
				if user.Username == ignoredUser {
					ignored = true
					break
				}
			}
			if !ignored && !user.Blocked {
				remainingUsers = append(remainingUsers, user)
			}
		}

		// Если остались пользователи, создаём клавиатуру
		if len(remainingUsers) > 0 {
			keyboard := t.createUserSelectionKeyboard(remainingUsers, true)
			editMsg := tgbotapi.NewEditMessageReplyMarkup(chatID, callback.Message.MessageID, keyboard)
			if _, err := bot.Send(editMsg); err != nil {
				log.Errorf("Failed to edit message reply markup: %v", err)
			}
		} else {
			// Если пользователей больше нет, отправляем сообщение об отправке
			t.messageState[chatID] = ""
			t.sendMessageToUsers(chatID)
		}
	}
}

// Метод для создания inline-клавиатуры выбора пользователя для просмотра wishlist
func (t *Telegram) createUserSelectionKeyboardForWishlist(users []models.User) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton

	for _, user := range users {
		if !user.Blocked {
			// Используем префикс "view_wishlist:" для различения типа callback
			callbackData := fmt.Sprintf("view_wishlist:%d", user.TelegramID)
			button := tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("@%s", user.Username), callbackData)
			row := tgbotapi.NewInlineKeyboardRow(button)
			rows = append(rows, row)
		}
	}

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}
