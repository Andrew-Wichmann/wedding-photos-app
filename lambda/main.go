package main

import (
	"context"
	_ "embed"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

//go:embed index.html
var indexHTML string

func handler(ctx context.Context, request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	switch request.RequestContext.HTTP.Method {
	case "GET":
		return handleGET(request)
	case "POST":
		return handlePOST(request)
	default:
		return events.LambdaFunctionURLResponse{Body: "Method not supported", Headers: map[string]string{}, StatusCode: 400}, nil
	}
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

func handlePOST(request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	return events.LambdaFunctionURLResponse{}, nil
}

func main() {
	lambda.Start(handler)
}
