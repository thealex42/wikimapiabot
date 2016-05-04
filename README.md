# wikimapiabot

Telegram bot for Wikimapia

** See it in action: @wikimapia_bot (telegram) ** 

Build and run

Before running edit wikimapiabot.go and set wikimapia and telegram api keys

**1) Docker**

docker build -t wikimapiabot -f Dockerfile .

docker run -it --name wikimapiabot wikimapiabot

**2) Go**

go get; go build; ./wikimapiabot


**TODO**

* tests
* use goroutines pool in main loop
