.PHONY: build clean deploy

build:
	cd lambda && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -ldflags '-extldflags "-static"' -o main main.go
	cd lambda && zip main.zip main

clean:
	rm -f lambda/main lambda/main.zip

deploy: build
	cd terraform && terraform apply