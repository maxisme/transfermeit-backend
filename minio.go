package main

import (
	"context"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const bucketName = "tmi"

func getMinioClient(endpoint, bucket, accessKeyID, secretAccessKey string) (*minio.Client, error) {
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, err
	}

	err = minioClient.MakeBucket(context.Background(), bucket, minio.MakeBucketOptions{ObjectLocking: true})
	if err != nil {
		// Check to see if we already own this bucket
		exists, err := minioClient.BucketExists(context.Background(), bucket)
		if err != nil && !exists {
			return nil, err
		} else if exists {
			fmt.Printf("bucket '%s' already exists...\n", bucket)
		}
	}
	return minioClient, nil
}
