.PHONY: build clean deploy setup-rekognition

build:
	cd lambda-app && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -ldflags '-extldflags "-static"' -o bootstrap main.go
	cd lambda-app && zip main.zip bootstrap index.html
	cd lambda-metadata && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -ldflags '-extldflags "-static"' -o bootstrap main.go
	cd lambda-metadata && zip main.zip bootstrap

clean:
	rm -f lambda-app/bootstrap lambda-app/main.zip
	rm -f lambda-metadata/bootstrap lambda-metadata/main.zip

setup-rekognition:
	@echo "Creating Rekognition face collection 'wedding-faces'..."
	aws rekognition create-collection --collection-id wedding-faces --region us-east-1 || echo "Collection may already exist"

deploy: build
	cd terraform && terraform apply