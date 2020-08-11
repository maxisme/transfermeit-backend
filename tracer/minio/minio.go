package minio

import (
	"context"
	"fmt"
	"github.com/maxisme/transfermeit-backend/tracer"
	"github.com/minio/minio-go/v7"
	"go.opentelemetry.io/otel/api/kv"
	"go.opentelemetry.io/otel/api/trace"
	"io"
	"net/http"
)

func GetObject(r *http.Request, m *minio.Client, ctx context.Context, bucketName, objectName string,
	opts minio.GetObjectOptions) (*minio.Object, error) {
	span := getMinioSpan(r, m, "minio GetObject", bucketName, objectName)
	defer span.End()
	return m.GetObject(ctx, bucketName, objectName, opts)
}

func PutObject(r *http.Request, m *minio.Client, ctx context.Context, bucketName, objectName string,
	reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	span := getMinioSpan(r, m, "minio PutObject", bucketName, objectName)
	defer span.End()
	return m.PutObject(ctx, bucketName, objectName, reader, objectSize, opts)
}

func RemoveObject(r *http.Request, m *minio.Client, ctx context.Context, bucketName, objectName string,
	opts minio.RemoveObjectOptions) error {
	span := getMinioSpan(r, m, "minio RemoveObject", bucketName, objectName)
	defer span.End()
	return m.RemoveObject(ctx, bucketName, objectName, opts)
}

func getMinioSpan(r *http.Request, m *minio.Client, spanName, bucketName, objectName string) trace.Span {
	span := tracer.GetSpan(r, spanName)
	span.SetAttributes(
		kv.Key("bucket").String(bucketName),
		kv.Key("endpoint").String(fmt.Sprintf("%v", m.EndpointURL())),
		kv.Key("object").String(fmt.Sprintf("%v", objectName)))
	return span
}
