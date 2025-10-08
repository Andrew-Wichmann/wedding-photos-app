package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/aws/aws-sdk-go/service/rekognition"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/rwcarlsen/goexif/exif"
)

type FaceDetail struct {
	FaceID     string  `json:"faceId"`
	Confidence float64 `json:"confidence"`
	BoundingBox struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
		Left   float64 `json:"left"`
		Top    float64 `json:"top"`
	} `json:"boundingBox"`
	AgeRange *struct {
		Low  int64 `json:"low"`
		High int64 `json:"high"`
	} `json:"ageRange,omitempty"`
	Gender     string  `json:"gender,omitempty"`
	Smile      bool    `json:"smile,omitempty"`
	Emotions   []string `json:"emotions,omitempty"`
}

type PhotoMetadata struct {
	PhotoID       string       `json:"photoId"`
	UploadedAt    int64        `json:"uploadedAt"`
	DateTaken     string       `json:"dateTaken,omitempty"`
	Make          string       `json:"make,omitempty"`
	Model         string       `json:"model,omitempty"`
	Latitude      float64      `json:"latitude,omitempty"`
	Longitude     float64      `json:"longitude,omitempty"`
	Altitude      float64      `json:"altitude,omitempty"`
	FocalLength   string       `json:"focalLength,omitempty"`
	FNumber       string       `json:"fNumber,omitempty"`
	ExposureTime  string       `json:"exposureTime,omitempty"`
	ISO           int          `json:"iso,omitempty"`
	Width         int          `json:"width,omitempty"`
	Height        int          `json:"height,omitempty"`
	Orientation   int          `json:"orientation,omitempty"`
	FileSize      int64        `json:"fileSize"`
	Faces         []FaceDetail `json:"faces,omitempty"`
	FaceCount     int          `json:"faceCount"`
}

func handler(ctx context.Context, s3Event events.S3Event) error {
	sess := session.Must(session.NewSession())
	s3Client := s3.New(sess)
	dynamoClient := dynamodb.New(sess)
	rekognitionClient := rekognition.New(sess)
	tableName := os.Getenv("DYNAMODB_TABLE")
	collectionID := "wedding-faces"

	for _, record := range s3Event.Records {
		bucket := record.S3.Bucket.Name
		key := record.S3.Object.Key
		size := record.S3.Object.Size

		log.Printf("Processing: s3://%s/%s (size: %d bytes)", bucket, key, size)

		// Download file from S3
		result, err := s3Client.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			log.Printf("Error downloading %s: %v", key, err)
			continue
		}

		// Create temp file
		tempFile, err := os.CreateTemp("", "photo-*")
		if err != nil {
			log.Printf("Error creating temp file: %v", err)
			result.Body.Close()
			continue
		}
		tempPath := tempFile.Name()

		// Copy S3 object to temp file
		_, err = io.Copy(tempFile, result.Body)
		result.Body.Close()
		tempFile.Close()
		if err != nil {
			log.Printf("Error writing temp file: %v", err)
			os.Remove(tempPath)
			continue
		}

		// Extract EXIF metadata
		metadata := extractMetadata(tempPath, key, size)
		os.Remove(tempPath)

		// Index faces with Rekognition
		faces, err := indexFaces(rekognitionClient, bucket, key, collectionID)
		if err != nil {
			log.Printf("Error indexing faces for %s: %v", key, err)
		} else {
			metadata.Faces = faces
			metadata.FaceCount = len(faces)
			log.Printf("Indexed %d faces for %s", len(faces), key)
		}

		// Store in DynamoDB
		av, err := dynamodbattribute.MarshalMap(metadata)
		if err != nil {
			log.Printf("Error marshaling metadata: %v", err)
			continue
		}

		_, err = dynamoClient.PutItem(&dynamodb.PutItemInput{
			TableName: aws.String(tableName),
			Item:      av,
		})
		if err != nil {
			log.Printf("Error storing metadata in DynamoDB: %v", err)
			continue
		}

		log.Printf("Successfully processed %s", key)
	}

	return nil
}

func extractMetadata(filePath, key string, fileSize int64) PhotoMetadata {
	metadata := PhotoMetadata{
		PhotoID:    key,
		UploadedAt: time.Now().Unix(),
		FileSize:   fileSize,
	}

	// Open file and decode EXIF
	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("Error opening file for EXIF: %v", err)
		return metadata
	}
	defer f.Close()

	x, err := exif.Decode(f)
	if err != nil {
		log.Printf("No EXIF data found in %s: %v", key, err)
		return metadata
	}

	// Extract camera info
	if make, err := x.Get(exif.Make); err == nil {
		if val, err := make.StringVal(); err == nil {
			metadata.Make = val
		}
	}

	if model, err := x.Get(exif.Model); err == nil {
		if val, err := model.StringVal(); err == nil {
			metadata.Model = val
		}
	}

	// Extract date/time
	if dt, err := x.DateTime(); err == nil {
		metadata.DateTaken = dt.Format(time.RFC3339)
	}

	// Extract GPS coordinates
	lat, lon, err := x.LatLong()
	if err == nil {
		metadata.Latitude = lat
		metadata.Longitude = lon
	}

	// Extract camera settings
	if focalLength, err := x.Get(exif.FocalLength); err == nil {
		if val, err := focalLength.Rat(0); err == nil {
			f, _ := val.Float64()
			metadata.FocalLength = fmt.Sprintf("%.1fmm", f)
		}
	}

	if fNumber, err := x.Get(exif.FNumber); err == nil {
		if val, err := fNumber.Rat(0); err == nil {
			f, _ := val.Float64()
			metadata.FNumber = fmt.Sprintf("f/%.1f", f)
		}
	}

	if exposureTime, err := x.Get(exif.ExposureTime); err == nil {
		if val, err := exposureTime.Rat(0); err == nil {
			metadata.ExposureTime = fmt.Sprintf("%d/%d", val.Num(), val.Denom())
		}
	}

	if iso, err := x.Get(exif.ISOSpeedRatings); err == nil {
		if val, err := iso.Int(0); err == nil {
			metadata.ISO = val
		}
	}

	// Extract dimensions
	if width, err := x.Get(exif.PixelXDimension); err == nil {
		if val, err := width.Int(0); err == nil {
			metadata.Width = val
		}
	}

	if height, err := x.Get(exif.PixelYDimension); err == nil {
		if val, err := height.Int(0); err == nil {
			metadata.Height = val
		}
	}

	if orientation, err := x.Get(exif.Orientation); err == nil {
		if val, err := orientation.Int(0); err == nil {
			metadata.Orientation = val
		}
	}

	return metadata
}

func indexFaces(client *rekognition.Rekognition, bucket, key, collectionID string) ([]FaceDetail, error) {
	// Call Rekognition IndexFaces to add faces to collection
	input := &rekognition.IndexFacesInput{
		CollectionId: aws.String(collectionID),
		Image: &rekognition.Image{
			S3Object: &rekognition.S3Object{
				Bucket: aws.String(bucket),
				Name:   aws.String(key),
			},
		},
		DetectionAttributes: []*string{
			aws.String("ALL"), // Include age, gender, emotions, etc.
		},
		MaxFaces:       aws.Int64(10), // Max faces to index per photo
		QualityFilter:  aws.String("AUTO"),
	}

	result, err := client.IndexFaces(input)
	if err != nil {
		return nil, fmt.Errorf("failed to index faces: %w", err)
	}

	var faces []FaceDetail
	for _, faceRecord := range result.FaceRecords {
		face := FaceDetail{
			FaceID:     *faceRecord.Face.FaceId,
			Confidence: *faceRecord.Face.Confidence,
		}

		// Extract bounding box
		if faceRecord.FaceDetail.BoundingBox != nil {
			bb := faceRecord.FaceDetail.BoundingBox
			face.BoundingBox.Width = *bb.Width
			face.BoundingBox.Height = *bb.Height
			face.BoundingBox.Left = *bb.Left
			face.BoundingBox.Top = *bb.Top
		}

		// Extract age range
		if faceRecord.FaceDetail.AgeRange != nil {
			face.AgeRange = &struct {
				Low  int64 `json:"low"`
				High int64 `json:"high"`
			}{
				Low:  *faceRecord.FaceDetail.AgeRange.Low,
				High: *faceRecord.FaceDetail.AgeRange.High,
			}
		}

		// Extract gender
		if faceRecord.FaceDetail.Gender != nil && faceRecord.FaceDetail.Gender.Value != nil {
			face.Gender = *faceRecord.FaceDetail.Gender.Value
		}

		// Extract smile
		if faceRecord.FaceDetail.Smile != nil && faceRecord.FaceDetail.Smile.Value != nil {
			face.Smile = *faceRecord.FaceDetail.Smile.Value
		}

		// Extract top emotions
		var emotions []string
		for _, emotion := range faceRecord.FaceDetail.Emotions {
			if emotion.Confidence != nil && *emotion.Confidence > 50 {
				emotions = append(emotions, *emotion.Type)
			}
		}
		face.Emotions = emotions

		faces = append(faces, face)
	}

	return faces, nil
}

func main() {
	lambda.Start(handler)
}
