package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
)

type CheckRequest struct {
	Request CheckRequestRequest `json:"request"`
}
type CheckRequestRequest struct {
	Method     string            `json:"method"`
	Body       CheckRequestBody  `json:"body"`
	SecureInfo RequestSecureInfo `json:"secureinfo"`
}
type CheckRequestBody struct {
	File string `json:"file"`
}
type RequestSecureInfo struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type CheckResponse struct {
	Response CheckResponseResponse `json:"response"`
}
type CheckResponseResponse struct {
	RequestId int64 `json:"requestId"`
}

type PollRequest struct {
	Request PollRequestRequest `json:"request"`
}
type PollRequestRequest struct {
	Method     string            `json:"method"`
	Body       PollRequestBody   `json:"body"`
	SecureInfo RequestSecureInfo `json:"secureinfo"`
}
type PollRequestBody struct {
	RequestId int64  `json:"requestId"`
	Format    string `json:"format"`
}

type PollResponse struct {
	Response PollResponseResponse `json:"response"`
}
type PollResponseResponse struct {
	Result PollResponseResult `json:"result"`
}
type PollResponseResult struct {
	OriginalityRating float64 `json:"originality_rating"`
}

const (
	SendFileToCheck = "like"
	PollForResult   = "getresult"
	DeleteResult    = "delresult"
)

type UserCommand struct {
	Command string
	File    string
}

func main() {
	const botToken = ""
	const secureInfoLogin = ""
	const secureInfoPassword = ""
	const antiPlagiatSite = ""
	secureInfo := &RequestSecureInfo{
		Login:    secureInfoLogin,
		Password: secureInfoPassword,
	}

	bot, err := tgbotapi.NewBotAPI("MyAwesomeBotToken")
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil { // If we got a message
			log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

			messageToSend, requestId, err := handleMessage(secureInfo, antiPlagiatSite, update.Message)
			if err != nil {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Incorrect command")
				msg.ReplyToMessageID = update.Message.MessageID

				bot.Send(msg)
				continue
			}
			bot.Send(messageToSend)

			go pollResult(requestId, secureInfo, antiPlagiatSite, bot, update.Message)
		}
	}
}

func getUserCommand(message string) *UserCommand {
	messageParts := strings.Split(message, " ")
	res := &UserCommand{
		Command: messageParts[0],
		File:    strings.Join(messageParts[1:], " "),
	}
	return res
}

func handleMessage(secureInfo *RequestSecureInfo, antiPlagiatSite string, message *tgbotapi.Message) (*tgbotapi.MessageConfig, int64, error) {
	command := getUserCommand(message.Text)
	if command.Command == "/check" {
		requestJson, err := json.Marshal(&CheckRequest{
			Request: CheckRequestRequest{
				Method: SendFileToCheck,
				Body: CheckRequestBody{
					File: command.File,
				},
				SecureInfo: *secureInfo,
			}})
		if err != nil {
			return nil, 0, fmt.Errorf("couldn't parse request to json %w", err)
		}
		request, err := http.NewRequest("POST", antiPlagiatSite, bytes.NewBuffer(requestJson))
		request.Header.Set("Content-Type", "application/json; charset=UTF-8")

		client := &http.Client{}
		response, err := client.Do(request)
		if err != nil {
			return nil, 0, fmt.Errorf("http error %w", err)
		}
		defer response.Body.Close()

		if response.StatusCode != 200 {
			return nil, 0, fmt.Errorf("status code != 200")
		}

		checkResponseJson, _ := ioutil.ReadAll(response.Body)

		var resp CheckResponse
		err = json.Unmarshal(checkResponseJson, &resp)
		if err != nil {
			return nil, 0, fmt.Errorf("couldn't unparse %w", err)
		}

		msg := tgbotapi.NewMessage(message.Chat.ID, message.Text)
		msg.ReplyToMessageID = message.MessageID
		return &msg, resp.Response.RequestId, nil
	} else {
		return nil, 0, fmt.Errorf("incorrect command")
	}
}

func pollResult(requestId int64, secureInfo *RequestSecureInfo, antiPlagiatSite string, bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	requestJson, err := json.Marshal(&PollRequest{
		Request: PollRequestRequest{
			Method: PollForResult,
			Body: PollRequestBody{
				RequestId: requestId,
				Format:    "json",
			},
			SecureInfo: *secureInfo,
		},
	})
	if err != nil {
		return
	}
	request, err := http.NewRequest("POST", antiPlagiatSite, bytes.NewBuffer(requestJson))
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")

	client := &http.Client{}

	for attemptCount := 0; attemptCount < 5; attemptCount++ {
		time.Sleep(10 * time.Second)
		response, err1 := client.Do(request)
		if err1 != nil {
			continue
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			continue
		}

		checkResponseJson, _ := ioutil.ReadAll(response.Body)

		var resp PollResponse
		err = json.Unmarshal(checkResponseJson, &resp)
		if err != nil {
			continue
		}

		response.Body.Close()

		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("%f", resp.Response.Result.OriginalityRating))
		msg.ReplyToMessageID = message.MessageID

		bot.Send(msg)

		return
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, "couldn't get result")
	msg.ReplyToMessageID = message.MessageID

	bot.Send(msg)
}
