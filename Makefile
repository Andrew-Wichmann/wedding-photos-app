.PHONY: build clean deploy setup-rekognition setup-backend init-backend

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

setup-backend:
	@echo "Setting up S3 backend for Terraform state..."
	@echo "Step 1: Creating backend infrastructure..."
	cd terraform && terraform init
	cd terraform && terraform apply -target=aws_s3_bucket.terraform_state -target=aws_s3_bucket_versioning.terraform_state -target=aws_s3_bucket_server_side_encryption_configuration.terraform_state -target=aws_s3_bucket_public_access_block.terraform_state -target=aws_dynamodb_table.terraform_locks -auto-approve
	@echo ""
	@echo "Step 2: Uncommenting backend configuration..."
	sed -i 's/  # backend "s3" {/  backend "s3" {/' terraform/main.tf
	sed -i 's/  #   bucket/    bucket/' terraform/main.tf
	sed -i 's/  #   key/    key/' terraform/main.tf
	sed -i 's/  #   region/    region/' terraform/main.tf
	sed -i 's/  #   dynamodb_table/    dynamodb_table/' terraform/main.tf
	sed -i 's/  #   encrypt/    encrypt/' terraform/main.tf
	sed -i 's/  # }/  }/' terraform/main.tf
	@echo "Step 3: Migrating state to S3 backend..."
	cd terraform && terraform init -migrate-state -force-copy
	@echo ""
	@echo "✅ Backend setup complete! State is now stored in S3."

deploy: build
	cd terraform && terraform apply