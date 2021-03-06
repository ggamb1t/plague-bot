package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
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
	Document   CheckRequestBodyDocument   `json:"doc"`
	Parameters CheckRequestBodyParameters `json:"parameters"`
}
type CheckRequestBodyDocument struct {
	Filename string `json:"filename"`
	Body     string `json:"body"`
}
type CheckRequestBodyParameters struct {
	Year int32 `json:"year"`
}
type RequestSecureInfo struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type CheckResponse struct {
	Response CheckResponseResponse `json:"reponse"`
}
type CheckResponseResponse struct {
	Error     ErrorResponse `json:"error"`
	RequestId int64         `json:"requestId"`
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
	Response PollResponseResponse `json:"reponse"`
}
type PollResponseResponse struct {
	Error  ErrorResponse      `json:"error"`
	Result PollResponseResult `json:"result"`
}
type PollResponseResult struct {
	OriginalityRating float64 `json:"originality_rating"`
}

type ErrorResponse struct {
	Code    int32  `json:"code"`
	Message string `json:"message"`
}

const (
	SendFileToCheck = "like"
	PollForResult   = "getresult"
	DeleteResult    = "delresult"
)

const (
	retryCount = 5
)

const (
	commandStart = "/start"
	commandCheck = "/check"
)

type UserCommand struct {
	Command  string
	Year     int32
	Filename string
	File     string
}

type Secrets struct {
	BotToken           string `json:"botToken"`
	SecureInfoLogin    string `json:"secureInfoLogin"`
	SecureInfoPassword string `json:"secureInfoPassword"`
	AntiPlagiatSite    string `json:"antiPlagiatSite"`
}

func main() {
	file, err := os.Open("secret.json")
	if err != nil {
		log.Panic(err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	var secrets Secrets
	err = decoder.Decode(&secrets)
	if err != nil {
		log.Panic(err)
	}

	secureInfo := &RequestSecureInfo{
		Login:    secrets.SecureInfoLogin,
		Password: secrets.SecureInfoPassword,
	}

	bot, err := tgbotapi.NewBotAPI(secrets.BotToken)
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

			messageToSend, requestId, err := handleMessage(secureInfo, secrets.AntiPlagiatSite, update.Message)
			if err != nil {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, err.Error())
				msg.ReplyToMessageID = update.Message.MessageID

				bot.Send(msg)
				continue
			}
			bot.Send(messageToSend)

			if requestId != 0 {
				go pollResult(requestId, secureInfo, secrets.AntiPlagiatSite, bot, update.Message)
			}
		}
	}
}

func getUserCommand(message *tgbotapi.Message) (*UserCommand, error) {
	messageParts := strings.Split(message.Text, " ")

	if !contains(getCommands(), messageParts[0]) {
		return nil, fmt.Errorf("incorrect command")
	}
	if messageParts[0] == commandStart {
		res := &UserCommand{
			Command: commandStart,
		}
		return res, nil
	}
	year, err := strconv.Atoi(messageParts[1])
	if err != nil {
		return nil, err
	}

	file := base64.StdEncoding.EncodeToString(bytes.NewBufferString(strings.Join(messageParts[3:], " ")).Bytes())

	res := &UserCommand{
		Command:  commandCheck,
		Year:     int32(year),
		Filename: messageParts[2],
		File:     file,
	}
	return res, nil
}

func handleMessage(secureInfo *RequestSecureInfo, antiPlagiatSite string, message *tgbotapi.Message) (*tgbotapi.MessageConfig, int64, error) {
	command, err := getUserCommand(message)
	if err != nil {
		return nil, 0, err
	}
	if command.Command == commandStart {
		msg := tgbotapi.NewMessage(message.Chat.ID, "/check year filename text\nExample:\n/check 2022 diplom.txt Hello Exa")
		msg.ReplyToMessageID = message.MessageID
		return &msg, 0, nil
	}
	requestJson, err := json.Marshal(&CheckRequest{
		Request: CheckRequestRequest{
			Method: SendFileToCheck,
			Body: CheckRequestBody{
				Document: CheckRequestBodyDocument{
					Filename: command.Filename,
					Body:     command.File,
				},
				Parameters: CheckRequestBodyParameters{
					Year: command.Year,
				},
			},
			SecureInfo: *secureInfo,
		}})
	if err != nil {
		return nil, 0, fmt.Errorf("couldn't parse request to json %w", err)
	}
	request, err := http.NewRequest("POST", antiPlagiatSite, bytes.NewBuffer(requestJson))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request %w", err)
	}
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

	checkResponseJson, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read body %w", err)
	}

	var resp CheckResponse
	err = json.Unmarshal(checkResponseJson, &resp)
	if err != nil {
		return nil, 0, fmt.Errorf("couldn't unparse %w", err)
	}
	if resp.Response.Error.Code != 0 {
		return nil, 0, fmt.Errorf("error from server: %s", resp.Response.Error.Message)
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, message.Text)
	msg.ReplyToMessageID = message.MessageID
	return &msg, resp.Response.RequestId, nil
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
	if err != nil {
		log.Printf("%s", err)
	}
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")

	client := &http.Client{}

	for attemptCount := 0; attemptCount < retryCount; attemptCount++ {
		time.Sleep(10 * time.Second)
		response, err := client.Do(request)
		if err != nil {
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
		if resp.Response.Error.Code != 0 {
			continue
		}

		response.Body.Close()

		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf(
			"request id: %d\noriginality rating: %f",
			requestId,
			resp.Response.Result.OriginalityRating))
		msg.ReplyToMessageID = message.MessageID

		bot.Send(msg)

		return
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, "couldn't get result")
	msg.ReplyToMessageID = message.MessageID

	bot.Send(msg)
}

func getCommands() []string {
	return []string{
		commandStart,
		commandCheck,
	}
}

func contains(s []string, searchterm string) bool {
	found := false
	for _, s2 := range s {
		if s2 == searchterm {
			found = true
		}
	}
	return found
}
