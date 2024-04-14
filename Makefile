#!make

.PHONY: build-armv7
build-armv7:
	GOOS=linux GOARCH=arm GOARM=7 go build -o ai-irc-bot-arm ai-irc-bot.go

.PHONY: build-armv6
build-armv6:
	GOOS=linux GOARCH=arm GOARM=6 go build -o ai-irc-bot-arm ai-irc-bot.go
