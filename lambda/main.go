package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

//go:embed index.html
var indexHTML string

type UploadRequest struct {
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
}

type UploadResponse struct {
	UploadURL string `json:"uploadUrl"`
	Key       string `json:"key"`
}

func (f UploadRequest) myfunct() {

}

func handler(ctx context.Context, request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	// Route based on path and method
	path := request.RequestContext.HTTP.Path
	method := request.RequestContext.HTTP.Method

	if method == "GET" && path == "/" {
		return handleGET(request)
	}

	if method == "POST" && path == "/upload" {
		return handleUpload(request)
	}

	if method == "GET" && path == "/gallery" {
		return handleGallery(request)
	}

	return events.LambdaFunctionURLResponse{
		StatusCode: 404,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       `{"error": "Not found"}`,
	}, nil
}

func handleGET(request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type": "text/html; charset=utf-8",
		},
		Body: indexHTML,
	}, nil
}

func handleUpload(request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	// Parse request body
	var uploadReq UploadRequest
	if err := json.Unmarshal([]byte(request.Body), &uploadReq); err != nil {
		return events.LambdaFunctionURLResponse{
			StatusCode: 400,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"error": "Invalid JSON"}`,
		}, nil
	}

	if uploadReq.FileName == "" {
		return events.LambdaFunctionURLResponse{
			StatusCode: 400,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"error": "fileName is required"}`,
		}, nil
	}

	// Initialize AWS session
	sess := session.Must(session.NewSession())
	s3Client := s3.New(sess)
	bucketName := os.Getenv("S3_BUCKET")

	// Generate unique key with timestamp
	timestamp := time.Now().Unix()
	key := fmt.Sprintf("uploads/%d-%s", timestamp, uploadReq.FileName)

	// Create pre-signed PUT request
	req, _ := s3Client.PutObjectRequest(&s3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(key),
		ContentType: aws.String(uploadReq.ContentType),
	})

	// Generate pre-signed URL valid for 15 minutes
	uploadURL, err := req.Presign(15 * time.Minute)
	if err != nil {
		return events.LambdaFunctionURLResponse{
			StatusCode: 500,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"error": "Failed to generate upload URL"}`,
		}, nil
	}

	// Return pre-signed URL and key
	response := UploadResponse{
		UploadURL: uploadURL,
		Key:       key,
	}

	responseBody, _ := json.Marshal(response)

	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type":                 "application/json",
			"Access-Control-Allow-Origin":  "*",
			"Access-Control-Allow-Methods": "POST, OPTIONS",
			"Access-Control-Allow-Headers": "Content-Type",
		},
		Body: string(responseBody),
	}, nil
}

func handleGallery(request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	// Initialize AWS session
	sess := session.Must(session.NewSession())
	s3Client := s3.New(sess)
	bucketName := os.Getenv("S3_BUCKET")

	// List all objects in the uploads folder
	result, err := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String("uploads/"),
	})

	if err != nil {
		return events.LambdaFunctionURLResponse{
			StatusCode: 500,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       `{"error": "Failed to list files"}`,
		}, nil
	}

	// Build list of file URLs
	type GalleryItem struct {
		Key          string `json:"key"`
		URL          string `json:"url"`
		LastModified string `json:"lastModified"`
		Size         int64  `json:"size"`
	}

	var items []GalleryItem
	for _, obj := range result.Contents {
		// Generate pre-signed URL for viewing (valid for 1 hour)
		req, _ := s3Client.GetObjectRequest(&s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    obj.Key,
		})
		url, err := req.Presign(1 * time.Hour)
		if err != nil {
			continue
		}

		items = append(items, GalleryItem{
			Key:          *obj.Key,
			URL:          url,
			LastModified: obj.LastModified.Format(time.RFC3339),
			Size:         *obj.Size,
		})
	}

	responseBody, _ := json.Marshal(items)

	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers: map[string]string{
			"Content-Type":                 "application/json",
			"Access-Control-Allow-Origin":  "*",
			"Access-Control-Allow-Methods": "GET, OPTIONS",
			"Access-Control-Allow-Headers": "Content-Type",
			"Cache-Control":                "no-cache, no-store, must-revalidate",
			"Pragma":                       "no-cache",
			"Expires":                      "0",
		},
		Body: string(responseBody),
	}, nil
}

func main() {
	lambda.Start(handler)
}
