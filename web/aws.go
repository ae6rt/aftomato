package main

import (
	"io/ioutil"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/s3"
)

type StorageService interface {
	GetBuildsByProject(project Project, sinceUnixTime uint64, limit uint64) ([]Build, error)
	GetArtifacts(buildID string) ([]byte, error)
	GetConsoleLog(buildID string) ([]byte, error)
}

type AWSStorageService struct {
	Config *aws.Config
}

func NewAWSStorageService(accessKey, accessSecret, awsRegion string) StorageService {
	key, err := ioutil.ReadFile("/etc/secrets/aws-key")
	if err != nil {
		Log.Printf("No /etc/secrets/aws-key.  Falling back to provided default: %v\n", err)
		key = []byte(accessKey)
	}

	secret, err := ioutil.ReadFile("/etc/secrets/aws-secret")
	if err != nil {
		Log.Printf("No /etc/secrets/aws-secret.  Falling back to provided default: %v\n", err)
		secret = []byte(accessSecret)
	}

	config := aws.NewConfig().WithCredentials(credentials.NewStaticCredentials(string(key), string(secret), "")).WithRegion(awsRegion).WithMaxRetries(3)
	return AWSStorageService{Config: config}
}

func (c AWSStorageService) GetBuildsByProject(project Project, since uint64, limit uint64) ([]Build, error) {
	svc := dynamodb.New(c.Config)
	params := &dynamodb.QueryInput{
		TableName:              aws.String("decap-build-metadata"),
		IndexName:              aws.String("projectKey-buildTime-index"),
		KeyConditionExpression: aws.String("projectKey = :pkey"),
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":pkey": {
				S: aws.String(project.Key),
			},
		},
	}

	resp, err := svc.Query(params)

	if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			// Generic AWS error with Code, Message, and original error (if any)
			Log.Println(awsErr.Code(), awsErr.Message(), awsErr.OrigErr())
			if reqErr, ok := err.(awserr.RequestFailure); ok {
				// A service error occurred
				Log.Println(reqErr.Code(), reqErr.Message(), reqErr.StatusCode(), reqErr.RequestID())
			}
		} else {
			// This case should never be hit, the SDK should always return an
			// error which satisfies the awserr.Error interface.
			Log.Println(err.Error())
		}
		return nil, err
	}

	builds := make([]Build, 0)
	for _, v := range resp.Items {
		buildElapsedTime, err := strconv.ParseUint(*v["buildElapsedTime"].N, 10, 64)
		if err != nil {
			Log.Printf("Error converting buildElapsedTime to ordinal value: %v\n", err)
		}
		buildResult, err := strconv.ParseInt(*v["buildResult"].N, 10, 32)
		if err != nil {
			Log.Printf("Error converting buildResult to ordinal value: %v\n", err)
		}
		buildTime, err := strconv.ParseUint(*v["buildTime"].N, 10, 64)
		if err != nil {
			Log.Printf("Error converting buildTime to ordinal value: %v\n", err)
		}

		build := Build{
			ID:       *v["buildID"].S,
			Branch:   *v["branch"].S,
			Duration: buildElapsedTime,
			Result:   int(buildResult),
			UnixTime: buildTime,
		}
		builds = append(builds, build)
	}
	return builds, nil
}

func (c AWSStorageService) GetArtifacts(buildID string) ([]byte, error) {
	return c.bytesFromBucket("decap-build-artifacts", buildID)
}

func (c AWSStorageService) GetConsoleLog(buildID string) ([]byte, error) {
	return c.bytesFromBucket("decap-console-logs", buildID)
}

func (c AWSStorageService) bytesFromBucket(bucketName, objectKey string) ([]byte, error) {
	svc := s3.New(c.Config)

	params := &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	}

	resp, err := svc.GetObject(params)
	if err != nil {
		Log.Println(err.Error())
		return nil, err
	}

	return ioutil.ReadAll(resp.Body)
}