package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/draychev/go-toolbox/pkg/logger"
	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
)

const (
	envOpenAIAPI     = "OPENAI_API_KEY"
	envIRCChannel    = "IRC_CHANNEL"
	envIRCServer     = "IRC_SERVER"
	envIRCNick       = "IRC_NICK"
	envIRCServerPass = "IRC_SERVER_PASSWORD"
)

var log = logger.NewPretty("asr33-irc")
var channel = os.Getenv(envIRCChannel)
var ircLogMu sync.Mutex

const ircLogFile = "IRC.log"

func generateRandomTwoDigit() string {
	rand.Seed(time.Now().UnixNano())
	number := rand.Intn(100)
	return fmt.Sprintf("%02d", number)
}

var irc = &ircevent.Connection{
	Server:        os.Getenv(envIRCServer),
	Nick:          fmt.Sprintf("%s%s", os.Getenv(envIRCNick), generateRandomTwoDigit()),
	RequestCaps:   []string{"server-time", "message-tags", "account-tag"},
	Password:      os.Getenv(envIRCServerPass),
	Debug:         true,
	KeepAlive:     60 * time.Second,
	Timeout:       45 * time.Second,
	ReconnectFreq: 3 * time.Second,
}

func checkEnvVars(vars []string) {
	for _, v := range vars {
		if os.Getenv(v) == "" {
			log.Fatal().Msgf("Please set env var %s", v)
		}
	}
}

func appendIRCLog(nick, message string) {
	ircLogMu.Lock()
	defer ircLogMu.Unlock()

	f, err := os.OpenFile(ircLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Error().Err(err).Msg("Failed to open IRC log file")
		return
	}
	defer f.Close()

	ts := time.Now().Format(time.RFC3339)
	clean := strings.ReplaceAll(message, "\n", " ")
	clean = strings.TrimSpace(clean)
	_, _ = fmt.Fprintf(f, "%s\t%s\t%s\n", ts, nick, clean)
}

// Define a structure for individual messages in the request
type Message struct {
	Role    string `json:"role"` // e.g., "user" or "system"
	Content string `json:"content"`
}

// Define the structure for the request body
type ChatGPTRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type APIError struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"` // Use *string to handle null value
	Code    string  `json:"code"`
}

// Define the structure for the response body
type ChatGPTResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *APIError `json:"error"`
}

const (
	maxIRCLen = 250
	prompt    = "You are replying in a public IRC channel. Output must be plain text only (no markdown, no code blocks, no colors, no emojis, no bullet lists). Keep the response to a single IRC message of at most 250 characters. Be concise."
)

// Function to send a question and receive a response
func AskChatGPT(question string) (string, error) {
	apiKey := os.Getenv(envOpenAIAPI)
	url := "https://api.openai.com/v1/chat/completions"

	reqBody := ChatGPTRequest{
		Model: "gpt-5.2",
		Messages: []Message{
			{
				Role:    "user",
				Content: fmt.Sprintf("%s: %s", prompt, question),
			},
		},
	}
	jsonReq, err := json.Marshal(reqBody)
	if err != nil {
		log.Error().Err(err).Msgf("Error marshaling body")
		return "", err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonReq))
	if err != nil {
		log.Error().Err(err).Msgf("Error making new request")
		return "", err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Error().Err(err).Msgf("Error making actual request")
		return "", err
	}
	defer resp.Body.Close()

	log.Info().Msg("ok")

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error().Err(err).Msgf("Error reading body")
		return "", err
	}

	var chatResp ChatGPTResponse
	err = json.Unmarshal(respBody, &chatResp)
	if err != nil {
		log.Error().Err(err).Msgf("Error unmarshaling: %s", respBody)
		return "", err
	}
	if chatResp.Error != nil {
		err := errors.New(chatResp.Error.Message)
		log.Error().Err(err).Msg("API Error")
		return "", err
	}

	// Return the first response (assuming there is at least one)
	if len(chatResp.Choices) > 0 {
		return chatResp.Choices[0].Message.Content, nil
	}
	return "No response received.", nil
}

func main() {
	checkEnvVars([]string{envIRCServer, envIRCNick, envIRCServerPass, envIRCChannel, envOpenAIAPI})
	/*
		answ, err := AskChatGPT("what time is it?")

		if err != nil {
			log.Error().Err(err).Msg("Error from OpenAI API")
		}
		log.Info().Msgf("Answer: %s", answ)
		log.Fatal().Msg("bye")
	*/
	irc.AddConnectCallback(func(e ircmsg.Message) {
		irc.Join(strings.TrimSpace(channel))
		// time.Sleep(3 * time.Second)
		// irc.Privmsg(channel, "hello")

	})

	irc.AddCallback("PRIVMSG", func(e ircmsg.Message) {
		message := e.Params[1]
		from := strings.Split(e.Source, "!")[0]
		log.Info().Msgf("%s: %s\n", from, message)
		target := e.Params[0]
		if target == channel {
			appendIRCLog(from, message)
		}

		nick := irc.Nick
		msgLower := strings.ToLower(message)
		nickLower := strings.ToLower(nick)
		var content string
		if strings.HasPrefix(msgLower, nickLower+":") || strings.HasPrefix(msgLower, nickLower+",") {
			content = strings.TrimSpace(message[len(nick)+1:])
		} else {
			return
		}

		log.Info().Msgf("%s: %s\n", from, content)

		log.Info().Msgf("Looking for answers for: %s", content)
		response, err := AskChatGPT(content)
		if err != nil {
			log.Error().Err(err).Msgf("Error asking ChatGPT: %s", message)
		} else {
			log.Info().Msgf("Response from ChatGPT: %s", response)
			trimmed := strings.ReplaceAll(response, "\n", " ")
			trimmed = strings.TrimSpace(trimmed)
			if len(trimmed) <= maxIRCLen {
				irc.Privmsg(channel, trimmed)
				return
			}
			from := 0
			maxLen := maxIRCLen
			for {
				irc.Privmsg(channel, trimmed[from:maxLen])
				time.Sleep(3 * time.Second)
				from = maxLen
				maxLen += 250
				if maxLen > len(trimmed) {
					irc.Privmsg(channel, trimmed[from:len(trimmed)])
					return
				}
			}
		}

	})

	if err := irc.Connect(); err != nil {
		log.Fatal().Err(err).Msgf("Could not connect to %s", irc.Server)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go irc.Loop()

	wg.Wait()
}
