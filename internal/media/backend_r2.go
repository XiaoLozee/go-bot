package media

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/XiaoLozee/go-bot/internal/config"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type r2Backend struct {
	bucket        string
	keyPrefix     string
	publicBaseURL string
	client        *s3.Client
}

func newR2Backend(ctx context.Context, cfg config.MediaR2Config) (*r2Backend, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		accountID := strings.TrimSpace(cfg.AccountID)
		if accountID == "" {
			return nil, fmt.Errorf("R2 endpoint 与 account_id 至少需要配置一项")
		}
		endpoint = "https://" + accountID + ".r2.cloudflarestorage.com"
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			strings.TrimSpace(cfg.AccessKeyID),
			strings.TrimSpace(cfg.SecretAccessKey),
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("初始化 R2 SDK 失败: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = &endpoint
	})

	return &r2Backend{
		bucket:        strings.TrimSpace(cfg.Bucket),
		keyPrefix:     strings.Trim(strings.ReplaceAll(cfg.KeyPrefix, "\\", "/"), "/"),
		publicBaseURL: strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
		client:        client,
	}, nil
}

func (b *r2Backend) Name() string {
	return "r2"
}

func (b *r2Backend) Save(ctx context.Context, asset Asset, staged stagedFile) (storedObject, error) {
	key := buildObjectKey(b.keyPrefix, asset, staged.Extension, staged.SHA256)
	file, err := os.Open(staged.TempPath)
	if err != nil {
		return storedObject{}, fmt.Errorf("打开待上传文件失败: %w", err)
	}
	defer func() { _ = file.Close() }()

	input := &s3.PutObjectInput{
		Bucket:        &b.bucket,
		Key:           &key,
		Body:          file,
		ContentLength: &staged.SizeBytes,
	}
	if strings.TrimSpace(staged.MimeType) != "" {
		input.ContentType = &staged.MimeType
	}
	if _, err := b.client.PutObject(ctx, input); err != nil {
		return storedObject{}, fmt.Errorf("上传 R2 失败: %w", err)
	}

	result := storedObject{Key: key}
	if b.publicBaseURL != "" {
		result.PublicURL = b.publicBaseURL + "/" + key
	}
	return result, nil
}
