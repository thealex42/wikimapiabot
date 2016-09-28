package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"

	"os"
	"path"

	log "github.com/Sirupsen/logrus"
	"github.com/ainar-g/i"
	"github.com/botanio/sdk/go"
	emoji "github.com/kyokomi/emoji"
	lediscfg "github.com/siddontang/ledisdb/config"
	"github.com/siddontang/ledisdb/ledis"
	"github.com/tucnak/telebot"
	"gopkg.in/caarlos0/env.v2"
	tgbotapi "gopkg.in/telegram-bot-api.v4"
)

type config struct {
	ApiKey   string `env:"API_KEY" envDefault:""`
	MapiaKey string `env:"WIKIMAPIA_KEY" envDefault:""`
	BotanKey string `env:"BOTAN_KEY" envDefault:""`
}

var (
	loggerError     = log.New()
	loggerLocations = log.New()

	cfg   config
	bot   *telebot.Bot
	mapia *Mapia
	db    *ledis.DB

	botanStat botan.Botan

	state map[int64]*MapiaPlaces // Keeps current state per chat users
)

func init() {
	cfg = config{}
	env.Parse(&cfg)

	logErrFd, err := os.OpenFile("logs/error.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	panicOnError(err)
	loggerError.Formatter = &log.JSONFormatter{}
	loggerError.Out = logErrFd

	logfd, err := os.OpenFile("logs/bot.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	panicOnError(err)
	log.SetFormatter(&log.JSONFormatter{})
	log.SetOutput(logfd)

	logLocFd, err := os.OpenFile("logs/loc.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	panicOnError(err)
	loggerLocations.Formatter = &log.JSONFormatter{}
	loggerLocations.Out = logLocFd

	i.LoadJSON("i18n/ru.json")

	mapia = NewMapia(cfg.MapiaKey)

	state = make(map[int64]*MapiaPlaces, 100)

	l, _ := ledis.Open(lediscfg.NewConfigDefault())
	db, _ = l.Select(0)
}

func main() {

	bot, err := tgbotapi.NewBotAPI(cfg.ApiKey)
	if err != nil {
		log.Panic(err)
	}

	botanStat = botan.New(cfg.BotanKey)

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	bot.Debug = true

	updates, err := bot.GetUpdatesChan(u)

	for update := range updates {

		func() {
			defer func() {
				if r := recover(); r != nil {

					loggerError.WithFields(log.Fields{
						"err":   r,
						"debug": string(debug.Stack()),
					}).Warn("PANIC")
					return
				}
			}()

			log.WithFields(log.Fields{
				"message": update.Message,
			}).Info("Update")

			// Handle user location
			if update.Message.Location != nil {

				places, err := mapia.GetNearbyObjects(
					update.Message.Location.Latitude,
					update.Message.Location.Longitude,
					getChatLocale(update.Message.Chat.ID))

				loggerLocations.WithFields(log.Fields{
					"lat":    update.Message.Location.Latitude,
					"lon":    update.Message.Location.Longitude,
					"userid": update.Message.Chat.ID,
					"nick":   update.Message.Chat.UserName,
					"fname":  update.Message.Chat.FirstName,
					"lname":  update.Message.Chat.LastName,
				}).Info("Location")

				if err != nil || places.Count == 0 {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, translate("No places found", update.Message.Chat.ID))
					bot.Send(msg)
				}

				state[update.Message.Chat.ID] = places

				text := placesToText(*places)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, text)
				msg.ReplyMarkup = getKeyboardMarkup(places.Count, update.Message.Chat.ID)

				bot.Send(msg)

				go func() {
					botanStat.Track(update.Message.From.ID, nil, "location")
				}()

				return
			}

			// Handle user selected object
			if userState, hasState := state[update.Message.Chat.ID]; hasState {
				if buttonPressed, hasButton := emojiMapReverse[update.Message.Text]; hasButton {

					fmt.Println("Loading!", buttonPressed)

					go func() {
						botanStat.Track(update.Message.From.ID, nil, "place")
					}()

					if buttonPressed <= 0 || buttonPressed-1 > len(userState.Places) {
						msg := tgbotapi.NewMessage(update.Message.Chat.ID,
							translate("Something went wrong", update.Message.Chat.ID))
						bot.Send(msg)
						return
					}

					placeToLoad := userState.Places[buttonPressed-1]

					placeData, err := mapia.GetPlaceById(placeToLoad.ID, getChatLocale(update.Message.Chat.ID))

					if err != nil {
						msg := tgbotapi.NewMessage(update.Message.Chat.ID, translate("Can't load place information", update.Message.Chat.ID))
						bot.Send(msg)
						return
					}

					placeText := placeDataToText(*placeData)
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, placeText)
					msg.ParseMode = "HTML"
					bot.Send(msg)

					// send pics
					cntSent := 0
					for pic := range placeData.Photos {

						if cntSent >= 3 {
							break
						}

						fileName, fileExt, err := downloadFile(placeData.Photos[pic].BigURL)
						if err != nil {
							continue
						}

						fileName2 := fmt.Sprintf("%s%s", fileName, fileExt)

						// rename file to give it extension
						err = os.Rename(fileName, fileName2)
						if err != nil {
							continue
						}

						msgp := tgbotapi.NewPhotoUpload(update.Message.Chat.ID, fileName2)
						bot.Send(msgp)
						cntSent++
					}

					return

				}
			}

			// Show language keyboard
			if strings.HasPrefix(update.Message.Text, "/lang") {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, translate("Choose language", update.Message.Chat.ID))
				msg.ReplyMarkup = getLanguagesMarkup()
				bot.Send(msg)
				return
			}

			if userLang, ok := langMapReverse[update.Message.Text]; ok {
				setChatLocale(update.Message.Chat.ID, userLang)
			}

			// Handle other messages
			log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, translate("Please share your location", update.Message.Chat.ID))

			msg.ReplyMarkup = getKeyboardMarkup(0, update.Message.Chat.ID)
			bot.Send(msg)

		}()
	}

}

func getChatLocale(chatId int64) string {

	data, err := db.Get([]byte(fmt.Sprintf("%v", chatId)))

	if err != nil {
		return "en"
	}

	return string(data)
}

func setChatLocale(chatId int64, locale string) {

	db.Set([]byte(fmt.Sprintf("%v", chatId)), []byte(locale))
}

func translate(msg string, chatId int64) string {
	return i.T(msg, "default", getChatLocale(chatId))
}

func placeDataToText(p MapiaPlace) string {

	var buffer bytes.Buffer

	descr := p.Description
	if len(descr) > 1024 {
		descr = substr(descr, 0, 1024) + "..."
	}

	buffer.WriteString(emoji.Sprintf(":house: <b>%s</b>\n\n", p.Title))
	buffer.WriteString(descr + "\n\n")
	buffer.WriteString(p.Urlhtml + "\n\n")

	return buffer.String()
}

func placesToText(p MapiaPlaces) string {

	if len(p.Places) == 0 {
		return ""
	}

	var buffer bytes.Buffer

	for i, v := range p.Places {
		buffer.WriteString(emoji.Sprint(emojiMap[i+1]))
		buffer.WriteString(v.Title)
		buffer.WriteString("\n\n")
	}

	return buffer.String()
}

// Generate buttons markup.
//
// Default button to share location is always present
// Specify number of digit buttons as argument
// Up to 9 buttons will be added, 3 per row

func getKeyboardMarkup(buttons int, chatId int64) *tgbotapi.ReplyKeyboardMarkup {
	ret := tgbotapi.ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]tgbotapi.KeyboardButton{
			[]tgbotapi.KeyboardButton{
				tgbotapi.KeyboardButton{Text: translate("Share location", chatId), RequestLocation: true},
			},
		},
	}

	if buttons > 0 {
		butRow := []tgbotapi.KeyboardButton{}

		for i := 0; i < buttons && i < 10; i++ {
			butRow = append(butRow, tgbotapi.KeyboardButton{Text: emoji.Sprint(emojiMap[i+1])})

			if (i+1)%3 == 0 {
				ret.Keyboard = append(ret.Keyboard, butRow)
				butRow = []tgbotapi.KeyboardButton{}
			}
		}

		if len(butRow) > 0 {
			ret.Keyboard = append(ret.Keyboard, butRow)
		}
	}

	return &ret
}

func getLanguagesMarkup() *tgbotapi.ReplyKeyboardMarkup {

	ret := tgbotapi.ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]tgbotapi.KeyboardButton{
			[]tgbotapi.KeyboardButton{
				tgbotapi.KeyboardButton{Text: emoji.Sprint(":ru:")},
				tgbotapi.KeyboardButton{Text: emoji.Sprint(":us:")},
			},
		},
	}

	return &ret
}

func downloadFile(remoteUrl string) (fileName string, ext string, err error) {

	parsed, err := url.Parse(remoteUrl)
	if err != nil {
		return "", "", err
	}

	ext = path.Ext(parsed.Path)

	// Create the file
	out, err := ioutil.TempFile("", "wiki")
	if err != nil {
		return "", "", err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(remoteUrl)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", "", err
	}

	return out.Name(), ext, nil
}

func panicOnError(err error) {
	if err != nil {
		panic(err)
	}
}
func substr(s string, pos, length int) string {
	runes := []rune(s)
	l := pos + length
	if l > len(runes) {
		l = len(runes)
	}
	return string(runes[pos:l])
}
